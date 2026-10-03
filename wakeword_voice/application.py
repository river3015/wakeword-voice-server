"""Single turn orchestration. Microphone is closed before AI processing/playback."""
from pathlib import Path
import tempfile
import tomllib

from .capture import CaptureSettings, LocalDetectors, collect, microphone_frames, wav_frames, write_wav
from .integrations import CliAI, Voicevox, play_wav
from .poc import PocError, transcribe


class Application:
    def __init__(self, config_path: Path, on_state=lambda state: None):
        try:
            with config_path.open('rb') as source:
                config = tomllib.load(source)
            self.config = config
            self.base = config_path.resolve().parent
            self.on_state = on_state
            provider = config.get('provider', 'codex')
            self.ai = CliAI(provider, config.get('ai_executable', provider), self.path(config['workdir']),
                            config.get('ai_timeout', 180), config.get('sandbox', 'read-only'))
            self.tts = Voicevox(config.get('voicevox_url', 'http://127.0.0.1:50021'), config.get('speaker', 0))
            self.capture_settings = CaptureSettings(**config.get('capture', {}))
        except (OSError, ValueError, KeyError, TypeError):
            raise PocError('設定ファイルを読み込めません') from None

    def path(self, value):
        path = Path(value).expanduser()
        return path if path.is_absolute() else self.base / path

    def recognize(self, source_wav=None, request_wav=None, listen_seconds=30):
        if request_wav:
            return self.transcribe(request_wav)
        try:
            detectors = LocalDetectors(self.path(self.config['wake_model']), self.path(self.config['vad_model']))
        except Exception:
            raise PocError('検出モデルを読み込めません') from None
        frames = wav_frames(source_wav) if source_wav else microphone_frames(self.config.get('device'), listen_seconds)
        try:
            utterance = collect(frames, detectors, self.capture_settings, self.on_state)
        finally:
            frames.close()
        if not utterance.pcm:
            return ''
        with tempfile.TemporaryDirectory(prefix='wakeword-request-') as folder:
            path = Path(folder) / 'request.wav'
            write_wav(path, utterance.pcm)
            return self.transcribe(path)

    def transcribe(self, path):
        self.on_state('transcribing')
        executable = self.config.get('whisper_cli', 'whisper-cli')
        if '/' in executable:
            executable = str(self.path(executable))
        return transcribe(path, self.path(self.config['whisper_model']), executable,
                          self.config.get('stt_timeout', 120), self.config.get('gpu', False))

    def turn(self, source_wav=None, request_wav=None, listen_seconds=30):
        try:
            text = self.recognize(source_wav, request_wav, listen_seconds)
            if not text:
                return {'completed': False, 'reason': 'no_speech'}
            self.on_state('processing')
            reply = self.ai.ask(text)
            self.on_state('synthesizing')
            speech = reply if len(reply) <= 1200 else reply[:1100] + '。返答が長いため読み上げを省略しました。'
            audio = self.tts.synthesize(speech)
            self.on_state('speaking')
            play_wav(audio)
            return {'completed': True, 'reply_characters': len(reply)}
        finally:
            self.on_state('waiting')
