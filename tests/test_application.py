from pathlib import Path
import unittest
from unittest.mock import Mock, patch
from wakeword_voice.application import Application
from wakeword_voice.poc import PocError


class ApplicationTests(unittest.TestCase):
    def make(self):
        states = []
        app = Application(Path('config.example.toml'), states.append)
        app.recognize = Mock(return_value='依頼です')
        app.ai = Mock()
        app.ai.ask.return_value = '返答です'
        app.tts = Mock()
        app.tts.synthesize.return_value = b'audio'
        return app, states

    def test_turn_plays_and_returns_to_waiting(self):
        app, states = self.make()
        with patch('wakeword_voice.application.play_wav') as play:
            result = app.turn()
        self.assertTrue(result['completed'])
        self.assertEqual(states, ['processing', 'synthesizing', 'speaking', 'waiting'])
        play.assert_called_once_with(b'audio')

    def test_failures_return_to_waiting(self):
        for part in ['recognize', 'ai', 'tts', 'play']:
            app, states = self.make()
            if part == 'recognize': app.recognize.side_effect = PocError('fail')
            if part == 'ai': app.ai.ask.side_effect = PocError('fail')
            if part == 'tts': app.tts.synthesize.side_effect = PocError('fail')
            with patch('wakeword_voice.application.play_wav', side_effect=PocError('fail') if part == 'play' else None):
                with self.assertRaises(PocError): app.turn()
            self.assertEqual(states[-1], 'waiting')

    def test_silence_never_calls_ai(self):
        app, states = self.make()
        app.recognize.return_value = ''
        self.assertFalse(app.turn()['completed'])
        app.ai.ask.assert_not_called()
