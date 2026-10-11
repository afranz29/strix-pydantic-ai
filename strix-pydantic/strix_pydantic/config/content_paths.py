"""Content path resolution for shared strix/ resources."""

import logging
import os
from pathlib import Path
from typing import Optional

logger = logging.getLogger(__name__)

# Required content directories for validation
REQUIRED_CONTENT_DIRS = {"tools", "skills", "agents"}


def resolve_content_dir() -> Optional[Path]:
    """
    Resolve strix content directory with priority:
    1. STRIX_CONTENT_DIR env var (if valid)
    2. Sibling ../strix/ from strix-pydantic package location
    3. Return None if not found

    Returns:
        Path to strix content root, or None if resolution fails
    """
    # Priority 1: Explicit env var override
    env_path = os.getenv("STRIX_CONTENT_DIR", "").strip()
    if env_path and Path(env_path).exists():
        if validate_content_dir(Path(env_path)):
            logger.info(f"Using content dir from STRIX_CONTENT_DIR: {env_path}")
            return Path(env_path)
        else:
            logger.warning(f"STRIX_CONTENT_DIR points to invalid path: {env_path}")

    # Priority 2: Sibling strix/ from package location
    # strix-pydantic/strix_pydantic/... -> find ../strix/
    package_dir = Path(__file__).parent.parent.parent  # strix-pydantic/
    sibling_strix = package_dir.parent / "strix"

    if sibling_strix.exists():
        if validate_content_dir(sibling_strix):
            logger.info(f"Using sibling strix content: {sibling_strix}")
            return sibling_strix
        else:
            logger.warning(f"Sibling strix exists but missing required dirs: {sibling_strix}")

    logger.warning("Could not resolve strix content directory. Set STRIX_CONTENT_DIR or ensure strix/ is a sibling of strix-pydantic/")
    return None


def validate_content_dir(path: Path) -> bool:
    """
    Validate that a path contains required content directories.

    Args:
        path: Path to validate

    Returns:
        True if all required directories exist, False otherwise
    """
    if not path.is_dir():
        return False

    for required_dir in REQUIRED_CONTENT_DIRS:
        if not (path / required_dir).is_dir():
            logger.warning(f"Missing required content directory: {required_dir}")
            return False

    return True


def get_content_path(subdir: str) -> Optional[Path]:
    """
    Get path to a content subdirectory (e.g., "skills", "tools", "agents").

    Args:
        subdir: Subdirectory name within content root

    Returns:
        Path to subdirectory, or None if content root not found
    """
    root = resolve_content_dir()
    if not root:
        return None

    subpath = root / subdir
    if not subpath.exists():
        logger.warning(f"Content subdirectory not found: {subpath}")
        return None

    return subpath
