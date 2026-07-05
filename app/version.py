"""构建期版本号读取。

优先级：环境变量 APP_VERSION > VERSION 文件 > 兜底默认值。
构建时由 Dockerfile 把 ${VERSION} 写入 app/VERSION，CI 决定 VERSION 取值：
  - tag 触发 (vX.Y.Z) → X.Y.Z
  - main 触发 → Development version
"""

import os
from pathlib import Path

_DEFAULT_VERSION = "Development version"
_VERSION_FILE = Path(__file__).parent / "VERSION"


def get_version() -> str:
    value = os.environ.get("APP_VERSION", "").strip()
    if value:
        return value
    try:
        text = _VERSION_FILE.read_text(encoding="utf-8").strip()
        if text:
            return text
    except FileNotFoundError:
        pass
    return _DEFAULT_VERSION