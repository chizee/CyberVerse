import os

import torch
import torch.distributed as dist
from loguru import logger


def load_pt_safetensors(in_path, remove_key=None, include_keys=None):
    include_keys = include_keys or []
    ext = os.path.splitext(in_path)[-1]
    if ext in (".pt", ".pth", ".tar"):
        state_dict = torch.load(in_path, map_location="cpu", weights_only=True)
        keys_to_keep = []
        for key in state_dict.keys():
            if include_keys:
                if any(inc_key in key for inc_key in include_keys):
                    keys_to_keep.append(key)
            else:
                if not (remove_key and remove_key in key):
                    keys_to_keep.append(key)
        state_dict = {k: state_dict[k] for k in keys_to_keep}
    else:
        import safetensors
        tensors = {}
        with safetensors.safe_open(in_path, framework="pt", device="cpu") as f:
            for key in f.keys():
                if include_keys:
                    if any(inc_key in key for inc_key in include_keys):
                        tensors[key] = f.get_tensor(key)
                else:
                    if not (remove_key and remove_key in key):
                        tensors[key] = f.get_tensor(key)
        state_dict = tensors
    return state_dict


def load_weights(checkpoint_path, cpu_offload=False, remove_key=None, load_from_rank0=False, include_keys=None):
    if not dist.is_initialized() or not load_from_rank0:
        logger.info(f"Loading weights from {checkpoint_path}")
        return load_pt_safetensors(checkpoint_path, remove_key, include_keys)

    is_weight_loader = dist.get_rank() == 0
    cpu_weight_dict = {}
    if is_weight_loader:
        logger.info(f"Loading weights from {checkpoint_path}")
        cpu_weight_dict = load_pt_safetensors(checkpoint_path, remove_key)

    meta_dict = {}
    if is_weight_loader:
        for key, tensor in cpu_weight_dict.items():
            meta_dict[key] = {"shape": tensor.shape, "dtype": tensor.dtype}

    obj_list = [meta_dict] if is_weight_loader else [None]
    dist.broadcast_object_list(obj_list, src=0)
    synced_meta_dict = obj_list[0]

    current_rank = dist.get_rank()
    if cpu_offload:
        target_device = "cpu"
        distributed_weight_dict = {
            key: torch.empty(meta["shape"], dtype=meta["dtype"], device=target_device)
            for key, meta in synced_meta_dict.items()
        }
        dist.barrier()
    else:
        target_device = torch.device(f"cuda:{current_rank}")
        distributed_weight_dict = {
            key: torch.empty(meta["shape"], dtype=meta["dtype"], device=target_device)
            for key, meta in synced_meta_dict.items()
        }
        dist.barrier(device_ids=[torch.cuda.current_device()])

    for key in sorted(synced_meta_dict.keys()):
        tensor_to_broadcast = distributed_weight_dict[key]
        if is_weight_loader:
            tensor_to_broadcast.copy_(cpu_weight_dict[key], non_blocking=True)
        if cpu_offload:
            if is_weight_loader:
                gpu_tensor = tensor_to_broadcast.cuda()
                dist.broadcast(gpu_tensor, src=0)
                tensor_to_broadcast.copy_(gpu_tensor.cpu(), non_blocking=True)
                del gpu_tensor
                torch.cuda.empty_cache()
            else:
                gpu_tensor = torch.empty_like(tensor_to_broadcast, device="cuda")
                dist.broadcast(gpu_tensor, src=0)
                tensor_to_broadcast.copy_(gpu_tensor.cpu(), non_blocking=True)
                del gpu_tensor
                torch.cuda.empty_cache()
        else:
            dist.broadcast(tensor_to_broadcast, src=0)

    if is_weight_loader:
        del cpu_weight_dict
    if cpu_offload:
        torch.cuda.empty_cache()

    logger.info(f"Weights distributed across {dist.get_world_size()} devices on {target_device}")
    return distributed_weight_dict
