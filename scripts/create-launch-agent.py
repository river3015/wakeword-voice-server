"""Generate a reviewable LaunchAgent; does not install or enable it."""
from pathlib import Path
import plistlib

root = Path(__file__).resolve().parents[1]
output = root / '.runtime/local.wakeword-voice-server.plist'
output.parent.mkdir(exist_ok=True)
config = {
    'Label': 'local.wakeword-voice-server',
    'ProgramArguments': [str(root / '.venv/bin/python'), str(root / 'scripts/run-server.py')],
    'WorkingDirectory': str(root),
    'RunAtLoad': True,
    'KeepAlive': {'SuccessfulExit': False},
    'ThrottleInterval': 10,
    'StandardOutPath': str(root / '.runtime/server.log'),
    'StandardErrorPath': str(root / '.runtime/server-error.log'),
    'EnvironmentVariables': {'PATH': '/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin'},
}
output.write_bytes(plistlib.dumps(config))
print(output)
