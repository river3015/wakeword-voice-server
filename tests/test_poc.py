import tempfile
import unittest
import wave
from pathlib import Path
from unittest.mock import patch
import subprocess

from wakeword_voice.poc import PocError, transcribe, validate_wav


class PocTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.wav = self.root / "request with spaces.wav"
        with wave.open(str(self.wav), "wb") as output:
            output.setnchannels(1)
            output.setsampwidth(2)
            output.setframerate(16000)
            output.writeframes(bytes(3200))
        self.model = self.root / "model.bin"
        self.model.touch()

    def test_valid_wav(self):
        self.assertEqual(validate_wav(self.wav)["seconds"], 0.1)

    def test_wrong_rate_rejected(self):
        with wave.open(str(self.wav), "wb") as output:
            output.setnchannels(1)
            output.setsampwidth(2)
            output.setframerate(44100)
            output.writeframes(bytes(3200))
        with self.assertRaises(PocError):
            validate_wav(self.wav)

    def test_transcript_and_temporary_file_cleanup(self):
        outputs = []
        def run(arguments, **kwargs):
            self.assertEqual(arguments[arguments.index("-l") + 1], "ja")
            self.assertEqual(arguments[arguments.index("-f") + 1], str(self.wav.resolve()))
            target = Path(arguments[arguments.index("-of") + 1]).with_suffix(".txt")
            outputs.append(target)
            target.write_text("テストしてください\n", encoding="utf-8")
        with patch("wakeword_voice.poc.subprocess.run", side_effect=run):
            self.assertEqual(transcribe(self.wav, self.model, "whisper-cli"), "テストしてください")
        self.assertFalse(outputs[0].exists())

    def test_timeout_is_reported(self):
        with patch("wakeword_voice.poc.subprocess.run", side_effect=subprocess.TimeoutExpired("whisper", 1)):
            with self.assertRaisesRegex(PocError, "タイムアウト"):
                transcribe(self.wav, self.model, "whisper-cli", 1)

    def test_failure_does_not_expose_output(self):
        failure = subprocess.CalledProcessError(1, "whisper", stderr=b"private transcript")
        with patch("wakeword_voice.poc.subprocess.run", side_effect=failure):
            with self.assertRaises(PocError) as caught:
                transcribe(self.wav, self.model, "whisper-cli")
        self.assertNotIn("private", str(caught.exception))


if __name__ == "__main__":
    unittest.main()
