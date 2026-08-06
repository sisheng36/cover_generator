# Cover Generator

Cover Generator 是一个用于 Emby 媒体库封面生成和入库通知的 Go 工具，支持：

- 媒体库封面自动生成
- 手动上传图片生成封面
- Webhook 入库通知
- 新入库联动媒体库封面更新
- 定时任务自动更新封面

## 运行

### Docker Compose

```bash
docker compose up -d
```

默认访问地址：`http://localhost:8055`

### 本地运行

```bash
go run ./cmd/embytool
```

可用环境变量：

- `ADDR` 或 `PORT`：监听地址，默认 `8055`
- `APP_VERSION`：覆盖显示版本号

## 配置与数据

- 配置文件：`/data/config.json`
- 输入目录：`/data/input`
- 输出目录：`/data/covers_output`
- 静态资源：`static/`
- 字体：`fonts/`
- 示例图片：`images/`

## 仓库结构

- `cmd/embytool/`：程序入口
- `internal/`：核心实现
- `static/`：前端页面与站点资源
- `fonts/`、`images/`：运行所需资源

## 说明

默认端口为 `8055`。Docker 构建时会自动写入版本信息到 `VERSION`。

## License

本项目基于 [GNU General Public License v3.0](LICENSE) 开源发布。

灵感与借鉴：[Yahaha Cover Studio / justzerock/MoviePilot-Plugins](https://github.com/justzerock/MoviePilot-Plugins)（GPL v3.0）。
