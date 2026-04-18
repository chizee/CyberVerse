import os
from functools import lru_cache


@lru_cache(maxsize=None)
def GET_USE_CHANNELS_LAST_3D():
    return os.getenv("USE_CHANNELS_LAST_3D", "0") == "1"
