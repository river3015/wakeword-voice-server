from unittest import TestCase
from unittest.mock import Mock, patch
from pathlib import Path
from wakeword_voice.application import Application
from wakeword_voice.poc import PocError


class ServiceTests(TestCase):
    def make(self):
        events=[]
        app=Application(Path('config.example.toml'), events.append)
        return app, events

    def test_error_does_not_retry_request_and_service_recovers(self):
        app, events = self.make()
        app.turn=Mock(side_effect=[PocError('no connection'), {'completed':True}])
        results=[]
        sleeps=[]
        app.serve(max_cycles=2, on_result=results.append, sleep=sleeps.append)
        self.assertEqual(app.turn.call_count,2)
        self.assertEqual(results,[{'completed':True}])
        self.assertEqual(sleeps,[2])
        self.assertEqual(events[-1],'stopped')

    def test_shutdown_clears_history(self):
        app, events = self.make()
        app.conversation.remember('private', 'reply')
        app.turn=Mock(side_effect=KeyboardInterrupt)
        with self.assertRaises(KeyboardInterrupt): app.serve()
        self.assertFalse(app.conversation.turns)
        self.assertEqual(events[-1],'stopped')

    def test_end_conversation_does_not_call_ai(self):
        app, events = self.make()
        app.conversation.remember('previous', 'reply')
        app.recognize=Mock(return_value='会話終了。')
        app.ai=Mock()
        result=app.turn()
        self.assertEqual(result['reason'],'end_conversation')
        self.assertFalse(app.conversation.turns)
        app.ai.ask.assert_not_called()
