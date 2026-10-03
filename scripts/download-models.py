"""Fetch pinned test models, checking the hashes observed during local verification."""
import hashlib
import os
from pathlib import Path
import urllib.request

ROOT = Path(__file__).resolve().parents[1] / ".models"
BASE = "https://github.com/dscripka/openWakeWord/releases/download/v0.5.1/"
MODELS = [
    ("openwakeword/silero_vad.onnx", BASE + "silero_vad.onnx", "a35ebf52fd3ce5f1469b2a36158dba761bc47b973ea3382b3186ca15b1f5af28"),
    ("openwakeword/embedding_model.onnx", BASE + "embedding_model.onnx", "70d164290c1d095d1d4ee149bc5e00543250a7316b59f31d056cff7bd3075c1f"),
    ("openwakeword/melspectrogram.onnx", BASE + "melspectrogram.onnx", "ba2b0e0f8b7b875369a2c89cb13360ff53bac436f2895cced9f479fa65eb176f"),
    ("openwakeword/hey_jarvis_v0.1.onnx", BASE + "hey_jarvis_v0.1.onnx", "94a13cfe60075b132f6a472e7e462e8123ee70861bc3fb58434a73712ee0d2cb"),
    ("openwakeword/hey_mycroft_v0.1.onnx", BASE + "hey_mycroft_v0.1.onnx", "c2a311e8fa1338de89c31b3b46dc4dffd4af2f9a8d6ddead48893c2d301b1f18"),
    ("whisper/ggml-base.bin", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.bin", "60ed5bc3dd14eea856493d334349b405782ddcaf0028d4b5df4088345fba2efe"),
]


def digest(path):
    with path.open("rb") as source:
        return hashlib.file_digest(source, "sha256").hexdigest()


def main():
    for name, url, expected in MODELS:
        target = ROOT / name
        if target.is_file() and digest(target) == expected:
            print(f"確認済み: {name}")
            continue
        target.parent.mkdir(parents=True, exist_ok=True)
        temporary = target.with_suffix(".download")
        try:
            with urllib.request.urlopen(url, timeout=60) as source, temporary.open("wb") as output:
                while chunk := source.read(1024 * 1024):
                    output.write(chunk)
            if digest(temporary) != expected:
                raise ValueError(f"ハッシュ不一致: {name}")
            os.replace(temporary, target)
            print(f"取得済み: {name}")
        finally:
            temporary.unlink(missing_ok=True)


if __name__ == "__main__":
    main()
