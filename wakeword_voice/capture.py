"""Frame-based local recording with bounded storage and speech endpointing."""
from collections import deque
from dataclasses import dataclass
from pathlib import Path
from queue import Empty, Full, Queue
import time
import wave

from .poc import PocError, validate_wav

RATE = 16000
SAMPLES = 1280
FRAME_BYTES = SAMPLES * 2
FRAME_SECONDS = SAMPLES / RATE


@dataclass(frozen=True)
class CaptureSettings:
    wake_threshold: float = 0.5
    speech_threshold: float = 0.5
    silence_seconds: float = 1.2
    start_timeout: float = 5
    max_seconds: float = 30
    min_speech_seconds: float = 0.24

    def __post_init__(self):
        if not (0 < self.wake_threshold <= 1 and 0 < self.speech_threshold <= 1):
            raise PocError("検出閾値は0より大きく1以下にしてください")
        values = (self.silence_seconds, self.start_timeout, self.max_seconds, self.min_speech_seconds)
        if not all(0 < value <= 300 for value in values):
            raise PocError("録音時間の設定は0より大きく300秒以下にしてください")
        if self.min_speech_seconds > self.max_seconds:
            raise PocError("最小発話時間は録音上限以下にしてください")


@dataclass(frozen=True)
class Utterance:
    pcm: bytes
    reason: str


class Recorder:
    """Wake detection and speech collection; independent of inference and devices."""
    def __init__(self, settings: CaptureSettings):
        self.settings = settings
        self.reset()

    def reset(self):
        self.state = "waiting"
        self.frames = []
        self.preroll = deque(maxlen=4)
        self.elapsed = 0.0
        self.speech = 0.0
        self.silence = 0.0
        self.started = False

    def feed(self, pcm: bytes, wake_score: float, speech_score: float):
        if len(pcm) != FRAME_BYTES:
            raise PocError("入力音声フレームのサイズが不正です")
        if self.state == "waiting":
            if wake_score >= self.settings.wake_threshold:
                self.state = "recording"
            return None
        self.elapsed += FRAME_SECONDS
        speaking = speech_score >= self.settings.speech_threshold
        if speaking:
            if not self.started:
                self.frames.extend(self.preroll)
                self.preroll.clear()
            self.started = True
            self.speech += FRAME_SECONDS
            self.silence = 0.0
        elif self.started:
            self.silence += FRAME_SECONDS
        if self.started:
            self.frames.append(pcm)
        else:
            self.preroll.append(pcm)
        reason = None
        if not self.started and self.elapsed + 1e-9 >= self.settings.start_timeout:
            reason = "no_speech"
        elif self.elapsed + 1e-9 >= self.settings.max_seconds:
            reason = "max_duration"
        elif self.started and self.silence + 1e-9 >= self.settings.silence_seconds:
            reason = "silence"
        if reason:
            content = b"".join(self.frames) if self.speech + 1e-9 >= self.settings.min_speech_seconds else b""
            result = Utterance(content, reason if content else "no_speech")
            self.reset()
            return result
        return None


class LocalDetectors:
    def __init__(self, wake_model: Path, vad_model: Path):
        import numpy as np
        import onnxruntime as ort
        ort.disable_telemetry_events()
        from openwakeword.model import Model
        from openwakeword.vad import VAD
        self.np = np
        self.wake = Model(wakeword_models=[str(wake_model.resolve())], inference_framework="onnx",
                          melspec_model_path=str(wake_model.parent / "melspectrogram.onnx"),
                          embedding_model_path=str(wake_model.parent / "embedding_model.onnx"))
        self.vad = VAD(model_path=str(vad_model.resolve()))

    def scores(self, pcm: bytes, waiting: bool):
        audio = self.np.frombuffer(pcm, dtype="<i2")
        wake = max(float(value) for value in self.wake.predict(audio).values()) if waiting else 0.0
        speech = float(self.vad.predict(audio, frame_size=640)) if not waiting else 0.0
        return wake, speech

    def reset(self):
        self.wake.reset()
        self.vad.reset_states()


def wav_frames(path: Path):
    validate_wav(path)
    with wave.open(str(path), "rb") as source:
        while frame := source.readframes(SAMPLES):
            yield frame.ljust(FRAME_BYTES, b"\0")


def microphone_frames(device=None, max_seconds=None):
    """Close the stream when the consumer closes the generator; queue cannot grow."""
    try:
        import sounddevice as sd
    except ImportError:
        raise PocError("sounddeviceをインストールしてください") from None
    queue = Queue(maxsize=25)

    def callback(data, frames, timing, status):
        payload = None if status or frames != SAMPLES else bytes(data)
        try:
            queue.put_nowait(payload)
        except Full:
            # Backlog must never be mistaken for fresh live speech.
            try:
                queue.get_nowait()
                queue.put_nowait(None)
            except (Empty, Full):
                pass

    try:
        with sd.RawInputStream(samplerate=RATE, blocksize=SAMPLES, channels=1,
                               dtype="int16", device=device, callback=callback):
            start = time.monotonic()
            while max_seconds is None or time.monotonic() - start < max_seconds:
                try:
                    frame = queue.get(timeout=3)
                except Empty:
                    raise PocError("マイク入力が途切れました") from None
                if frame is None:
                    raise PocError("マイク入力の欠落を検出しました")
                yield frame
    except sd.PortAudioError:
        raise PocError("マイクを開けません。デバイスとmacOSのマイク許可を確認してください") from None


def collect(frames, detectors, settings, on_state=lambda state: None):
    recorder = Recorder(settings)
    on_state("waiting")
    for pcm in frames:
        before = recorder.state
        wake, speech = detectors.scores(pcm, recorder.state == "waiting")
        result = recorder.feed(pcm, wake, speech)
        if recorder.state != before:
            on_state(recorder.state)
        if result is not None:
            detectors.reset()
            return result
    return Utterance(b"", "input_ended")


def write_wav(path: Path, pcm: bytes):
    with wave.open(str(path), "wb") as output:
        output.setnchannels(1)
        output.setsampwidth(2)
        output.setframerate(RATE)
        output.writeframes(pcm)
