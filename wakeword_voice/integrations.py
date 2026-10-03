"""Official AI CLIs and loopback-only VOICEVOX; prompts never appear in argv."""
import json
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import urllib.error
import urllib.parse
import urllib.request

from .poc import PocError


class CliAI:
    def __init__(self, provider: str, executable: str, workdir: Path, timeout=180, sandbox="read-only"):
        if provider not in ("codex", "claude") or sandbox not in ("read-only", "workspace-write"):
            raise PocError("AI連携先または権限設定が不正です")
        if not workdir.is_dir() or not 0 < timeout <= 1800:
            raise PocError("AI作業ディレクトリまたはタイムアウトが不正です")
        self.provider, self.executable = provider, executable
        self.workdir, self.timeout, self.sandbox = workdir.resolve(), timeout, sandbox

    def ask(self, text: str) -> str:
        if not text.strip() or len(text) > 30000:
            raise PocError("AIに渡す依頼が空か長すぎます")
        prompt = "日本語で答えてください。音声で読み上げるため結論を先に短く述べ、コード全文や秘密値は返答に含めないでください。\n" + text
        with tempfile.TemporaryDirectory(prefix="wakeword-ai-") as folder:
            response_file = Path(folder) / "response.txt"
            if self.provider == "codex":
                command = [self.executable, "exec", "--ephemeral", "--ignore-user-config", "--sandbox", self.sandbox,
                           "-c", 'approval_policy="never"', "--color", "never", "-o", str(response_file), "-"]
            else:
                command = [self.executable, "-p", "--output-format", "text", "--no-session-persistence",
                           "--permission-mode", "default", "--disallowedTools", "mcp__*"]
                if self.sandbox == "read-only":
                    command += ["--tools", "Read,Glob,Grep"]
            try:
                with (Path(folder) / "stdout").open("wb") as output, (Path(folder) / "stderr").open("wb") as errors:
                    process = subprocess.Popen(command, cwd=self.workdir, stdin=subprocess.PIPE,
                                               stdout=output, stderr=errors, start_new_session=True)
                    try:
                        process.communicate(prompt.encode("utf-8"), timeout=self.timeout)
                    except (subprocess.TimeoutExpired, KeyboardInterrupt):
                        os.killpg(process.pid, signal.SIGTERM)
                        try:
                            process.wait(timeout=3)
                        except subprocess.TimeoutExpired:
                            os.killpg(process.pid, signal.SIGKILL)
                            process.wait()
                        raise
                if process.returncode:
                    raise PocError("AIの実行に失敗しました。認証・利用上限・承認条件を確認してください")
                if self.provider == "claude":
                    response_file = Path(folder) / "stdout"
                if not response_file.is_file() or response_file.stat().st_size > 65536:
                    raise PocError("AIの返答がないか長すぎます")
                reply = response_file.read_text(encoding="utf-8").strip()
                if not reply:
                    raise PocError("AIの返答が空です")
                return reply
            except subprocess.TimeoutExpired:
                raise PocError("AI処理がタイムアウトしました") from None
            except (OSError, UnicodeError):
                raise PocError("AI実行ファイルまたは返答を読み込めません") from None


class Voicevox:
    def __init__(self, url="http://127.0.0.1:50021", speaker=0, timeout=60):
        parsed = urllib.parse.urlparse(url)
        if parsed.scheme != "http" or parsed.hostname not in ("127.0.0.1", "localhost", "::1") or parsed.username or parsed.path not in ("", "/") or parsed.query or parsed.fragment:
            raise PocError("VOICEVOXはローカルHTTPアドレスを指定してください")
        if speaker < 0 or not 0 < timeout <= 300:
            raise PocError("VOICEVOX設定が不正です")
        self.url, self.speaker, self.timeout = url.rstrip("/"), speaker, timeout
        self.http = urllib.request.build_opener(urllib.request.ProxyHandler({}))

    def request(self, route, body=None):
        request = urllib.request.Request(self.url + route, data=body if body is not None else b"", method="POST",
                                         headers={"Content-Type": "application/json"})
        try:
            with self.http.open(request, timeout=self.timeout) as response:
                data = response.read(16 * 1024 * 1024 + 1)
            if len(data) > 16 * 1024 * 1024:
                raise PocError("VOICEVOXの返答が大きすぎます")
            return data
        except (urllib.error.URLError, TimeoutError, OSError):
            raise PocError("VOICEVOXへの接続・音声合成に失敗しました") from None

    def synthesize(self, text: str) -> bytes:
        if not text.strip() or len(text) > 1200:
            raise PocError("読み上げ文は1〜1200文字にしてください")
        query = urllib.parse.urlencode({"text": text, "speaker": self.speaker})
        try:
            audio_query = json.loads(self.request("/audio_query?" + query))
            audio = self.request(f"/synthesis?speaker={self.speaker}", json.dumps(audio_query).encode())
        except (ValueError, UnicodeError):
            raise PocError("VOICEVOXの応答形式が不正です") from None
        if not audio.startswith(b"RIFF") or audio[8:12] != b"WAVE":
            raise PocError("VOICEVOXからWAV音声を取得できません")
        return audio


def play_wav(audio: bytes):
    with tempfile.TemporaryDirectory(prefix="wakeword-play-") as folder:
        filename = Path(folder) / "reply.wav"
        filename.write_bytes(audio)
        try:
            subprocess.run(["/usr/bin/afplay", str(filename)], check=True, timeout=180,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except (subprocess.SubprocessError, OSError):
            raise PocError("音声を再生できません") from None
