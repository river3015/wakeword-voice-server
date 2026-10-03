"""No microphone, network, or AI invocation. Models are supplied by the caller."""
import subprocess
import tempfile
import wave
from pathlib import Path


class PocError(Exception):
    pass


def validate_wav(path: Path) -> dict:
    try:
        with wave.open(str(path), "rb") as source:
            if (source.getnchannels(), source.getsampwidth(), source.getframerate(), source.getcomptype()) != (1, 2, 16000, "NONE"):
                raise PocError("WAVは16kHz・モノラル・16bit PCMで用意してください")
            frames = source.getnframes()
            if frames == 0:
                raise PocError("WAVに音声フレームがありません")
            if len(source.readframes(frames)) != frames * 2:
                raise PocError("WAVの音声データが途中で切れています")
            return {"sample_rate": 16000, "frames": frames, "seconds": frames / 16000}
    except (OSError, wave.Error, EOFError):
        raise PocError("WAVを読み込めません") from None


def transcribe(wav: Path, model: Path, executable: str, timeout: float = 120) -> str:
    validate_wav(wav)
    if not model.is_file():
        raise PocError("Whisperモデルファイルがありません")
    with tempfile.TemporaryDirectory(prefix="wakeword-stt-") as temporary:
        output = Path(temporary) / "transcript"
        # A temporary output avoids parsing console timestamps and diagnostic logs.
        arguments = [executable, "-m", str(model.resolve()), "-f", str(wav.resolve()),
                     "-l", "ja", "-otxt", "-of", str(output)]
        try:
            subprocess.run(arguments, check=True, capture_output=True, timeout=timeout)
        except FileNotFoundError:
            raise PocError("whisper-cliがありません。--whisper-cliで実行ファイルを指定してください") from None
        except subprocess.TimeoutExpired:
            raise PocError("文字起こしがタイムアウトしました") from None
        except (subprocess.CalledProcessError, OSError):
            # Do not surface subprocess output: it may contain spoken private data.
            raise PocError("whisper-cliの実行に失敗しました") from None
        try:
            return output.with_suffix(".txt").read_text(encoding="utf-8").strip()
        except (OSError, UnicodeError):
            raise PocError("文字起こし結果を読み込めません") from None


def detect_wake(wav: Path, model: Path, threshold: float) -> dict:
    validate_wav(wav)
    if not 0 < threshold <= 1:
        raise PocError("thresholdは0より大きく1以下で指定してください")
    if not model.is_file() or model.suffix != ".onnx":
        raise PocError("openWakeWordのONNXモデルを指定してください")
    try:
        import numpy as np
        from openwakeword.model import Model
    except ImportError:
        raise PocError("wake用の依存パッケージがありません。PoC手順のセットアップを実行してください") from None
    try:
        detector = Model(wakeword_models=[str(model.resolve())], inference_framework="onnx")
        peak = 0.0
        triggered_at = None
        with wave.open(str(wav), "rb") as source:
            offset = 0
            while data := source.readframes(1280):
                samples = np.frombuffer(data, dtype="<i2")
                if len(samples) < 1280:
                    samples = np.pad(samples, (0, 1280 - len(samples)))
                scores = detector.predict(samples)
                score = max(float(value) for value in scores.values())
                peak = max(peak, score)
                if triggered_at is None and score >= threshold:
                    triggered_at = offset / 16000
                offset += 1280
        return {"detected": triggered_at is not None, "first_frame_seconds": triggered_at, "peak_score": peak}
    except Exception:
        raise PocError("ウェイクワード検出に失敗しました。モデルと共有モデルの配置を確認してください") from None
