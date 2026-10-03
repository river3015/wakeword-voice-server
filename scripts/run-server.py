"""Foreground supervisor: singleton lock, optional VOICEVOX, scoped sleep assertion."""
import fcntl
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]


def voicevox_ready():
    try:
        http = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with http.open('http://127.0.0.1:50021/version', timeout=1) as response:
            return response.status == 200
    except OSError:
        return False


def stop(process):
    if process and process.poll() is None:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()


def main():
    os.chdir(ROOT)
    (ROOT / '.runtime').mkdir(exist_ok=True)
    with (ROOT / '.runtime/server.lock').open('w') as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            print('このプロジェクトの音声サーバーは起動済みです', file=sys.stderr)
            return 1
        engine, server = None, None
        def interrupt(signum, frame):
            raise KeyboardInterrupt
        signal.signal(signal.SIGTERM, interrupt)
        try:
            if not voicevox_ready():
                engine = subprocess.Popen(['sh', str(ROOT / 'scripts/run-voicevox.sh')], start_new_session=True)
                for _ in range(60):
                    if voicevox_ready(): break
                    if engine.poll() is not None: raise RuntimeError('VOICEVOXが起動できません')
                    time.sleep(1)
                else:
                    raise RuntimeError('VOICEVOX起動がタイムアウトしました')
            command = ['/usr/bin/caffeinate', '-di', str(ROOT / '.venv/bin/python'), '-m',
                       'wakeword_voice', 'serve', '--config', str(ROOT / 'config.local.toml'), *sys.argv[1:]]
            server = subprocess.Popen(command, start_new_session=True)
            return server.wait()
        except KeyboardInterrupt:
            return 130
        except (OSError, RuntimeError) as error:
            print(str(error), file=sys.stderr)
            return 1
        finally:
            # SIGTERM during cleanup must not interrupt owned process cleanup.
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            stop(server)
            stop(engine)


if __name__ == '__main__':
    sys.exit(main())
