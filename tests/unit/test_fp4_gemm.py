import sys
from pathlib import Path

import pytest

torch = pytest.importorskip("torch")

LIVEACT_DIR = Path(__file__).resolve().parents[2] / "models" / "SoulX-LiveAct"
if str(LIVEACT_DIR) not in sys.path:
    sys.path.insert(0, str(LIVEACT_DIR))

import fp4_gemm  # noqa: E402


def _export_fp4_probe():
    class Module(torch.nn.Module):
        def forward(self, x):
            scale = torch.empty((), device=x.device, dtype=torch.float32)
            q_x, q_scale = fp4_gemm._scaled_nvfp4_quant(x, scale)
            return fp4_gemm._cutlass_scaled_nvfp4_mm(
                q_x,
                q_x,
                q_scale,
                q_scale,
                scale,
            )

    return torch.export.export(
        Module(),
        (torch.empty((4, 16), device="meta", dtype=torch.bfloat16),),
    )


def test_fp4_custom_ops_are_registered_when_supported():
    if not fp4_gemm._CUSTOM_OPS_AVAILABLE:
        pytest.skip("torch.library.custom_op is unavailable")

    assert hasattr(torch.ops.cyberverse_liveact, "scaled_nvfp4_quant")
    assert hasattr(torch.ops.cyberverse_liveact, "cutlass_scaled_nvfp4_mm")


def test_fp4_custom_ops_export_as_graph_ops():
    if not fp4_gemm._CUSTOM_OPS_AVAILABLE or not hasattr(torch, "export"):
        pytest.skip("torch custom op export is unavailable")

    exported = _export_fp4_probe()
    targets = {
        str(node.target)
        for node in exported.graph_module.graph.nodes
        if node.op == "call_function"
    }

    assert "cyberverse_liveact.scaled_nvfp4_quant.default" in targets
    assert "cyberverse_liveact.cutlass_scaled_nvfp4_mm.default" in targets


def test_fp4_custom_ops_fake_shapes_are_exportable():
    if not fp4_gemm._CUSTOM_OPS_AVAILABLE or not hasattr(torch, "export"):
        pytest.skip("torch custom op export is unavailable")

    exported = _export_fp4_probe()
    nodes = {
        str(node.target): node
        for node in exported.graph_module.graph.nodes
        if node.op == "call_function"
    }

    quant_out = nodes["cyberverse_liveact.scaled_nvfp4_quant.default"].meta["val"]
    assert quant_out[0].shape == (4, 8)
    assert quant_out[0].dtype == torch.uint8
    assert quant_out[1].shape == (128, 4)
    assert quant_out[1].dtype == torch.float8_e4m3fn

    mm_out = nodes["cyberverse_liveact.cutlass_scaled_nvfp4_mm.default"].meta["val"]
    assert mm_out.shape == (4, 4)
    assert mm_out.dtype == torch.bfloat16
