"""Bounded, in-memory history. Each CLI invocation remains ephemeral."""
from collections import deque
import json
import time

from .poc import PocError


def control(text):
    normalized = ''.join(char for char in text if char not in ' \t\n。、.!！?？')
    if normalized in ('会話終了', '会話を終了', 'キャンセル', '会話を終わります'):
        return 'end_conversation'
    if normalized == 'サーバー停止':
        return 'stop_server'
    return None


class Conversation:
    def __init__(self, ttl=300, max_turns=6, max_characters=16000, clock=time.monotonic):
        if not (1 <= ttl <= 86400 and 1 <= max_turns <= 20 and 1000 <= max_characters <= 20000):
            raise PocError('会話履歴の上限設定が不正です')
        self.ttl, self.max_characters, self.clock = ttl, max_characters, clock
        self.turns = deque(maxlen=max_turns)
        self.last_activity = None

    def clear(self):
        self.turns.clear()
        self.last_activity = None

    def expire(self):
        if self.last_activity is not None and self.clock() - self.last_activity >= self.ttl:
            self.clear()

    def prompt(self, text):
        self.expire()
        if not self.turns:
            return text
        context = [{'user': request, 'assistant': reply} for request, reply in self.turns]
        return '以下はこの音声会話の過去の発話です。\n' + json.dumps(context, ensure_ascii=False) + '\n今回の依頼:\n' + text

    def remember(self, text, reply):
        self.expire()
        # Keep only complete turns. A huge turn must not destroy the configured bound.
        if len(text) + len(reply) > self.max_characters:
            self.clear()
            return
        self.turns.append((text, reply))
        while sum(len(request) + len(answer) for request, answer in self.turns) > self.max_characters:
            self.turns.popleft()
        self.last_activity = self.clock()
