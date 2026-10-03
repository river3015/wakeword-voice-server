"""Single turn orchestration. Microphone is closed before AI processing/playback."""
from pathlib import Path
import tempfile
import tomllib
import time
import math

from .capture import CaptureSettings, LocalDetectors, collect, microphone_frames, wav_frames, write_wav
from .integrations import CliAI, Voicevox, play_wav, play_cue
from .poc import PocError, transcribe
from .conversation import Conversation, control


class Application:
    def __init__(self, config_path: Path, on_state=lambda state: None):
        try:
            with config_path.open('rb') as source:
                config = tomllib.load(source)
            self.config = config
            allowed = {'provider', 'ai_executable', 'workdir', 'sandbox', 'ai_timeout', 'wake_model',
                       'vad_model', 'whisper_model', 'whisper_cli', 'stt_timeout', 'gpu', 'receipt_cue',
                       'voicevox_url', 'speaker', 'voicevox_credit', 'device', 'capture', 'conversation'}
            if set(config) - allowed:
                raise PocError('設定ファイルに未対応の項目があります')
            self.base = config_path.resolve().parent
            self.on_state = on_state
            provider = config.get('provider', 'codex')
            self.ai = CliAI(provider, config.get('ai_executable', provider), self.path(config['workdir']),
                            config.get('ai_timeout', 180), config.get('sandbox', 'read-only'))
            self.tts = Voicevox(config.get('voicevox_url', 'http://127.0.0.1:50021'), config.get('speaker', 0))
            self.credit = config.get('voicevox_credit', 'VOICEVOX:四国めたん' if config.get('speaker', 0) == 0 else '')
            if not isinstance(self.credit, str) or not self.credit.strip():
                raise PocError('選択音声のvoicevox_creditを指定してください')
            self.capture_settings = CaptureSettings(**config.get('capture', {}))
            self.conversation = Conversation(**config.get('conversation', {}))
            self.detectors = None
            self.stopping = False
        except (OSError, ValueError, KeyError, TypeError):
            raise PocError('設定ファイルを読み込めません') from None

    def path(self, value):
        path = Path(value).expanduser()
        return path if path.is_absolute() else self.base / path

    def recognize(self, source_wav=None, request_wav=None, listen_seconds=30):
        if request_wav:
            return self.transcribe(request_wav)
        try:
            if self.detectors is None:
                self.detectors = LocalDetectors(self.path(self.config['wake_model']), self.path(self.config['vad_model']))
            detectors = self.detectors
            detectors.reset()
        except Exception:
            raise PocError('検出モデルを読み込めません') from None
        frames = wav_frames(source_wav) if source_wav else microphone_frames(self.config.get('device'), listen_seconds)
        try:
            utterance = collect(frames, detectors, self.capture_settings, self.capture_state)
        finally:
            frames.close()
        if not utterance.pcm:
            return ''
        with tempfile.TemporaryDirectory(prefix='wakeword-request-') as folder:
            path = Path(folder) / 'request.wav'
            write_wav(path, utterance.pcm)
            return self.transcribe(path)

    def capture_state(self, state):
        self.on_state(state)
        if state == 'recording' and self.config.get('receipt_cue', True):
            play_cue()

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
            action = control(text)
            if action:
                self.conversation.clear()
                self.stopping = action == 'stop_server'
                self.on_state(action)
                return {'completed': False, 'reason': action}
            self.on_state('processing')
            reply = self.ai.ask(self.conversation.prompt(text))
            self.on_state('synthesizing')
            speech = reply if len(reply) <= 1200 else reply[:1100] + '。返答が長いため読み上げを省略しました。'
            audio = self.tts.synthesize(speech)
            self.on_state('speaking')
            play_wav(audio)
            self.conversation.remember(text, reply)
            return {'completed': True, 'reply_characters': len(reply)}
        finally:
            self.on_state('waiting')

    def serve(self, listen_seconds=30, max_cycles=None, on_result=lambda result: None, sleep=time.sleep):
        if not math.isfinite(listen_seconds) or not 0 < listen_seconds <= 3600:
            raise PocError('listen-secondsは0より大きく3600秒以下にしてください')
        if max_cycles is not None and max_cycles <= 0:
            raise PocError('max-cyclesは正の回数にしてください')
        cycles, failures = 0, 0
        try:
            while not self.stopping and (max_cycles is None or cycles < max_cycles):
                try:
                    result = self.turn(listen_seconds=listen_seconds)
                    failures = 0
                    on_result(result)
                except PocError:
                    failures += 1
                    self.detectors = None
                    self.on_state('error')
                    sleep(min(2 ** min(failures, 4), 10))
                cycles += 1
        finally:
            self.conversation.clear()
            self.on_state('stopped')
