import unittest
from wakeword_voice.conversation import Conversation, control


class ConversationTests(unittest.TestCase):
    def test_history_and_expiry(self):
        now = [0]
        history = Conversation(ttl=10, clock=lambda: now[0])
        history.remember('合言葉は青い月', '覚えました')
        self.assertIn('青い月', history.prompt('合言葉は？'))
        now[0] = 10
        self.assertEqual(history.prompt('新しい依頼'), '新しい依頼')
        self.assertFalse(history.turns)

    def test_history_is_bounded(self):
        history = Conversation(max_turns=2, max_characters=1000)
        for number in range(5):
            history.remember(str(number) * 200, '答え' * 100)
        self.assertEqual(len(history.turns), 2)
        history.remember('x' * 1001, 'reply')
        self.assertFalse(history.turns)

    def test_control_requires_exact_phrase(self):
        self.assertEqual(control('会話終了。'), 'end_conversation')
        self.assertEqual(control('サーバー停止'), 'stop_server')
        self.assertIsNone(control('キャンセルについて説明して'))
