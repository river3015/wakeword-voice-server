import io
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
import wave
from wakeword_voice.integrations import CliAI, Voicevox
from wakeword_voice.poc import PocError


class IntegrationTests(unittest.TestCase):
    def test_remote_tts_rejected(self):
        for url in ['https://example.com', 'http://192.168.1.1:50021', 'http://localhost/remote', 'http://x@localhost']:
            with self.assertRaises(PocError):
                Voicevox(url)

    def test_voicevox_post_and_audio_format(self):
        memory = io.BytesIO()
        with wave.open(memory, 'wb') as out:
            out.setnchannels(1); out.setsampwidth(2); out.setframerate(24000)
            out.writeframes(bytes(240))
        client = Voicevox()
        with patch.object(client, 'request', side_effect=[b'{}', memory.getvalue()]) as call:
            result = client.synthesize('こんにちは')
        self.assertTrue(result.startswith(b'RIFF'))
        self.assertIn('speaker=0', call.call_args_list[0].args[0])
        with patch.object(client, 'request', side_effect=[b'{}', b'invalid']):
            with self.assertRaises(PocError):
                client.synthesize('こんにちは')

    def test_codex_prompt_stdin_and_ephemeral(self):
        commands = []
        class Process:
            returncode = 0
            def __init__(self, command, **kwargs):
                commands.append(command)
                self.target = Path(command[command.index('-o') + 1])
            def communicate(self, prompt, timeout):
                self.assert_prompt = prompt
                self.target.write_text('返答です', encoding='utf-8')
        with tempfile.TemporaryDirectory() as root, patch('wakeword_voice.integrations.subprocess.Popen', Process):
            self.assertEqual(CliAI('codex', 'codex', Path(root)).ask('private prompt'), '返答です')
        self.assertNotIn('private prompt', commands[0])
        self.assertIn('--ephemeral', commands[0])
        self.assertIn('read-only', commands[0])
        self.assertFalse(Path(commands[0][commands[0].index('-o')+1]).exists())

    def test_empty_prompt_and_invalid_settings(self):
        with tempfile.TemporaryDirectory() as root:
            with self.assertRaises(PocError):
                CliAI('codex', 'codex', Path(root)).ask(' ')
            with self.assertRaises(PocError):
                CliAI('codex', 'codex', Path(root), sandbox='danger-full-access')

    def test_actual_process_timeout(self):
        with tempfile.TemporaryDirectory() as folder:
            executable=Path(folder)/'slow-cli'
            executable.write_text('#!/bin/sh\nsleep 10\n')
            executable.chmod(0o755)
            with self.assertRaisesRegex(PocError,'タイムアウト'):
                CliAI('codex',str(executable),Path(folder),timeout=0.1).ask('test request')
