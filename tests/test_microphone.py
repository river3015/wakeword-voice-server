import sys
import types
from unittest import TestCase
from unittest.mock import patch
from wakeword_voice.capture import FRAME_BYTES, microphone_frames
from wakeword_voice.poc import PocError


class MicrophoneTests(TestCase):
    def device(self, status=False):
        class Stream:
            def __init__(self, **kwargs): self.callback=kwargs['callback']
            def __enter__(self):
                self.callback(bytes(FRAME_BYTES),1280,None,status)
                return self
            def __exit__(self, *args): pass
        return types.SimpleNamespace(RawInputStream=Stream,PortAudioError=type('PortAudioError',(Exception,),{}))

    def test_delayed_input_is_not_used(self):
        with patch.dict(sys.modules,{'sounddevice':self.device()}), patch('wakeword_voice.capture.time.monotonic',side_effect=[0,0,0,2]):
            with self.assertRaisesRegex(PocError,'遅延'):
                next(microphone_frames(max_seconds=3))

    def test_overflow_is_not_used(self):
        with patch.dict(sys.modules,{'sounddevice':self.device(status=True)}):
            with self.assertRaisesRegex(PocError,'欠落'):
                next(microphone_frames(max_seconds=3))
