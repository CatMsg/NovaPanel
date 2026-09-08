# NovaPanel 运行标识

| 项目 | 标识 |
| --- | --- |
| 管理命令 | `novas` |
| 程序 | `/usr/local/novas/novas` |
| systemd 服务 | `novas.service` |
| 默认数据库 | `/usr/local/novas/db/novas.db` |
| 更新状态和日志 | `/var/lib/novas/` |
| 本地开发 | `sh runNovas.sh` |
| 环境变量 | `NOVAS_*` |
| 备份下载名称 | `novas_日期.db` |

升级保留账号、配置、证书和用户数据，校验失败自动回退。
最近三份升级回退副本位于 `/var/lib/novas/update-backup/`，仅限管理员访问，不进入 Git 或发布包。
