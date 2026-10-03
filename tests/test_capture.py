import unittest
from wakeword_voice.capture import CaptureSettings, FRAME_BYTES, Recorder
from wakeword_voice.poc import PocError

FRAME = b'\x01\x00' * (FRAME_BYTES // 2)


class CaptureTests(unittest.TestCase):
    def make(self, **kwargs):
        return Recorder(CaptureSettings(silence_seconds=0.24, start_timeout=0.4,
                                        max_seconds=0.8, min_speech_seconds=0.16, **kwargs))

    def test_wake_is_not_part_of_recorded_request(self):
        recorder = self.make()
        recorder.feed(b'\0' * FRAME_BYTES, 1, 0)
        self.assertEqual(recorder.state, 'recording')
        recorder.feed(FRAME, 0, 1)
        recorder.feed(FRAME, 0, 1)
        result = None
        for _ in range(3):
            result = recorder.feed(FRAME, 0, 0)
        self.assertEqual(result.reason, 'silence')
        self.assertEqual(result.pcm, FRAME * 5)
        self.assertEqual(recorder.state, 'waiting')

    def test_no_speech_is_discarded(self):
        recorder = self.make()
        recorder.feed(FRAME, 1, 0)
        for _ in range(5):
            result = recorder.feed(FRAME, 0, 0)
        self.assertEqual(result.reason, 'no_speech')
        self.assertFalse(result.pcm)

    def test_continuous_speech_is_bounded(self):
        recorder = self.make()
        recorder.feed(FRAME, 1, 0)
        for _ in range(10):
            result = recorder.feed(FRAME, 0, 1)
        self.assertEqual(result.reason, 'max_duration')
        self.assertEqual(len(result.pcm), FRAME_BYTES * 10)

    def test_short_noise_is_not_a_request(self):
        recorder = self.make()
        recorder.feed(FRAME, 1, 0)
        recorder.feed(FRAME, 0, 1)
        for _ in range(3):
            result = recorder.feed(FRAME, 0, 0)
        self.assertFalse(result.pcm)

    def test_bad_settings_and_frames(self):
        with self.assertRaises(PocError):
            CaptureSettings(max_seconds=0)
        with self.assertRaises(PocError):
            self.make().feed(b'', 0, 0)
