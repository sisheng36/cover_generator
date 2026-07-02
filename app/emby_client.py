import logging
import requests
from typing import Dict, List, Any, Optional

logger = logging.getLogger(__name__)


class EmbyClient:
    def __init__(self, server_url: str, api_key: str):
        self.base_url = server_url.rstrip("/")
        self.api_key = api_key
        self._headers = {"X-Emby-Token": api_key, "Content-Type": "application/json"}

    def _get(self, path: str, params: dict = None) -> Optional[dict]:
        try:
            resp = requests.get(
                f"{self.base_url}{path}",
                params=params or {},
                headers=self._headers,
                timeout=30,
            )
            if resp.status_code == 200:
                return resp.json()
            logger.warning(f"Emby GET {path} -> {resp.status_code}")
        except Exception as e:
            logger.error(f"Emby GET {path} 失败: {e}")
        return None

    def get_user_id(self) -> Optional[str]:
        data = self._get("/Users")
        if isinstance(data, list) and data:
            for u in data:
                if u.get("Policy", {}).get("IsAdministrator"):
                    return u["Id"]
            return data[0]["Id"]
        return None

    def get_libraries(self) -> List[Dict]:
        uid = self.get_user_id()
        if not uid:
            return []
        data = self._get(f"/Users/{uid}/Views")
        if data:
            return data.get("Items", [])
        return []

    def get_library_items(
        self, library_id: str, limit: int = 10,
        sort_by: str = "Random", item_types: str = None,
        start_index: int = 0,
    ) -> List[Dict]:
        uid = self.get_user_id()
        if not uid:
            return []
        params = {
            "ParentId": library_id,
            "Limit": limit,
            "SortBy": sort_by,
            "SortOrder": "Descending",
            "Fields": (
                "Id,Name,Type,Path,ProviderIds,"
                "ImageTags,BackdropImageTags,PrimaryImageTag,PrimaryImageItemId,"
                "ParentBackdropImageTags,ParentBackdropItemId,"
                "SeriesPrimaryImageTag,SeriesId"
            ),
            "Recursive": True,
            "StartIndex": start_index,
        }
        if item_types:
            params["IncludeItemTypes"] = item_types
        if sort_by == "Random":
            params.pop("SortOrder", None)
        data = self._get(f"/Users/{uid}/Items", params)
        if data:
            return data.get("Items", [])
        return []

    def get_image_url(self, item: Dict, use_primary: bool = False) -> Optional[str]:
        item_id = item.get("Id")
        if not item_id:
            return None
        primary_url, backdrop_url, parent_backdrop_url, series_primary_url = None, None, None, None

        primary_tag = item.get("ImageTags", {}).get("Primary")
        if primary_tag:
            primary_url = f"/emby/Items/{item_id}/Images/Primary?tag={primary_tag}"
        else:
            ref_id = item.get("PrimaryImageItemId")
            ref_tag = item.get("PrimaryImageTag")
            if ref_id and ref_tag:
                primary_url = f"/emby/Items/{ref_id}/Images/Primary?tag={ref_tag}"

        backdrop_tags = item.get("BackdropImageTags") or []
        if backdrop_tags:
            backdrop_url = f"/emby/Items/{item_id}/Images/Backdrop/0?tag={backdrop_tags[0]}"

        parent_backdrop_tags = item.get("ParentBackdropImageTags") or []
        parent_backdrop_item_id = item.get("ParentBackdropItemId")
        if parent_backdrop_tags and parent_backdrop_item_id:
            parent_backdrop_url = (
                f"/emby/Items/{parent_backdrop_item_id}/Images/Backdrop/0?tag={parent_backdrop_tags[0]}"
            )

        series_primary_tag = item.get("SeriesPrimaryImageTag")
        series_id = item.get("SeriesId")
        if series_primary_tag and series_id:
            series_primary_url = f"/emby/Items/{series_id}/Images/Primary?tag={series_primary_tag}"

        if item.get("Type") == "Episode":
            if use_primary:
                return series_primary_url or primary_url or parent_backdrop_url or backdrop_url
            return parent_backdrop_url or backdrop_url or series_primary_url or primary_url

        if use_primary:
            return primary_url or parent_backdrop_url or backdrop_url
        return parent_backdrop_url or backdrop_url or primary_url

    def download_image(self, api_path: str, save_path: str) -> Optional[str]:
        try:
            url = f"{self.base_url}{api_path}"
            resp = requests.get(url, headers=self._headers, stream=True, timeout=30)
            if resp.status_code == 200:
                with open(save_path, "wb") as f:
                    for chunk in resp.iter_content(1024):
                        f.write(chunk)
                return save_path
            logger.warning(f"下载图片失败 {api_path} -> {resp.status_code}")
        except Exception as e:
            logger.error(f"下载图片异常 {api_path}: {e}")
        return None

    def upload_library_image(self, library_id: str, image_data: bytes) -> bool:
        try:
            from PIL import Image
            from io import BytesIO
            import base64 as b64
            img = Image.open(BytesIO(image_data))
            if img.mode == "RGBA":
                img = img.convert("RGB")
            buf = BytesIO()
            img.save(buf, format="JPEG", quality=95)
            body = b64.b64encode(buf.getvalue()).decode("utf-8")

            url = f"{self.base_url}/Items/{library_id}/Images/Primary"
            headers = {"X-Emby-Token": self.api_key, "Content-Type": "image/jpeg"}
            resp = requests.post(url, data=body, headers=headers, timeout=30)
            if resp.status_code in (200, 204):
                logger.info(f"封面上传成功: {library_id}")
                return True
            logger.warning(f"封面上传失败 {library_id} -> {resp.status_code}: {resp.text[:200]}")
        except Exception as e:
            logger.error(f"封面上传异常: {e}")
        return False
