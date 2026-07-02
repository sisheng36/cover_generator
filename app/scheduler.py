import logging
import threading
from typing import Dict, Any, List
from datetime import datetime

from apscheduler.schedulers.background import BackgroundScheduler
from apscheduler.triggers.cron import CronTrigger

logger = logging.getLogger(__name__)

_scheduler: BackgroundScheduler = None
_job_id = "cover_generator_scheduled"
# 必须是 RLock: start() 持锁时会调用 stop(),stop() 内部也要拿锁,普通 Lock 会自死锁
_job_lock = threading.RLock()


def start(config: dict, job_func):
    global _scheduler
    with _job_lock:
        stop()
        enabled = config.get("scheduler_enabled", False)
        cron_expr = config.get("scheduler_cron", "0 4 * * *")
        if not enabled:
            logger.info("定时任务未启用")
            return
        try:
            _scheduler = BackgroundScheduler(timezone="Asia/Shanghai")
            trigger = CronTrigger.from_crontab(cron_expr, timezone="Asia/Shanghai")
            _scheduler.add_job(
                func=job_func,
                trigger=trigger,
                id=_job_id,
                name="封面定时更新",
                replace_existing=True,
            )
            _scheduler.start()
            logger.info(f"定时任务已启动: cron={cron_expr}")
        except Exception as e:
            logger.error(f"定时任务启动失败: {e}")
            _scheduler = None


def stop():
    global _scheduler
    with _job_lock:
        if _scheduler and _scheduler.running:
            try:
                _scheduler.shutdown(wait=False)
                logger.info("定时任务已停止")
            except Exception as e:
                logger.warning(f"停止定时任务时出错: {e}")
        _scheduler = None


def restart(config: dict, job_func):
    start(config, job_func)


def is_running() -> bool:
    return _scheduler is not None and _scheduler.running


def get_next_run() -> str:
    if not is_running():
        return ""
    try:
        job = _scheduler.get_job(_job_id)
        if job and job.next_run_time:
            return job.next_run_time.strftime("%Y-%m-%d %H:%M:%S")
    except Exception:
        pass
    return ""
