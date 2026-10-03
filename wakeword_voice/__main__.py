import argparse
import json
import sys
import signal
from pathlib import Path

from .poc import PocError, detect_wake, transcribe, validate_wav


def main():
    def terminate(signum, frame):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, terminate)
    parser = argparse.ArgumentParser(description="録音済みWAVで音声処理を検証するPoC")
    commands = parser.add_subparsers(dest="command", required=True)
    check = commands.add_parser("check-wav")
    check.add_argument("wav", type=Path)
    wake = commands.add_parser("detect-wake")
    wake.add_argument("wav", type=Path)
    wake.add_argument("--model", type=Path, required=True)
    wake.add_argument("--threshold", type=float, default=0.5)
    stt = commands.add_parser("transcribe")
    stt.add_argument("wav", type=Path)
    stt.add_argument("--model", type=Path, required=True)
    stt.add_argument("--whisper-cli", default="whisper-cli")
    stt.add_argument("--timeout", type=float, default=120)
    stt.add_argument("--gpu", action="store_true", help="Metal GPUを使う（既定はCPU）")
    stt.add_argument("--show-text", action="store_true", help="文字起こしを標準出力に表示する")
    capture = commands.add_parser("capture", help="呼びかけ後の依頼を収集する（AI呼び出しなし）")
    capture.add_argument("--wake-model", type=Path, required=True)
    capture.add_argument("--vad-model", type=Path, required=True)
    capture.add_argument("--source-wav", type=Path, help="マイクの代わりにWAVを使う")
    capture.add_argument("--device", type=int)
    capture.add_argument("--listen-seconds", type=float, default=30)
    capture.add_argument("--save-audio", type=Path, help="明示した場合のみ依頼音声を保存する")
    once = commands.add_parser("once", help="日本語依頼→AI→VOICEVOXを一往復する")
    once.add_argument("--config", type=Path, required=True)
    inputs = once.add_mutually_exclusive_group()
    inputs.add_argument("--source-wav", type=Path, help="呼びかけを含むWAV")
    inputs.add_argument("--request-wav", type=Path, help="呼びかけなしの日本語依頼WAV")
    once.add_argument("--listen-seconds", type=float, default=30)
    serve = commands.add_parser("serve", help="会話履歴を保持して継続待ち受けする")
    serve.add_argument("--config", type=Path, required=True)
    serve.add_argument("--listen-seconds", type=float, default=30)
    serve.add_argument("--max-cycles", type=int, help="検証用の待ち受け回数上限")
    args = parser.parse_args()
    try:
        if args.command in ("once", "serve"):
            from .application import Application
            if not 0 < args.listen_seconds <= 3600:
                raise PocError("listen-secondsは0より大きく3600秒以下にしてください")
            app = Application(args.config, lambda state: print(json.dumps({"state": state}), file=sys.stderr))
            print("音声: " + app.credit, file=sys.stderr)
            if args.command == "serve":
                app.serve(args.listen_seconds, args.max_cycles,
                          lambda result: print(json.dumps(result), flush=True))
                result = {"stopped": True}
            else:
                result = app.turn(args.source_wav, args.request_wav, args.listen_seconds)
        elif args.command == "check-wav":
            result = validate_wav(args.wav)
        elif args.command == "detect-wake":
            result = detect_wake(args.wav, args.model, args.threshold)
        elif args.command == "capture":
            from .capture import CaptureSettings, LocalDetectors, collect, microphone_frames, wav_frames, write_wav
            if not 0 < args.listen_seconds <= 3600:
                raise PocError("listen-secondsは0より大きく3600秒以下にしてください")
            try:
                detectors = LocalDetectors(args.wake_model, args.vad_model)
            except Exception:
                raise PocError("検出モデルを読み込めません。依存とモデル配置を確認してください") from None
            frames = wav_frames(args.source_wav) if args.source_wav else microphone_frames(args.device, args.listen_seconds)
            try:
                utterance = collect(frames, detectors, CaptureSettings(),
                                    lambda state: print(json.dumps({"state": state}), file=sys.stderr))
            finally:
                frames.close()
            if args.save_audio and utterance.pcm:
                write_wav(args.save_audio, utterance.pcm)
            result = {"captured": bool(utterance.pcm), "reason": utterance.reason,
                      "seconds": len(utterance.pcm) / 32000}
        else:
            if args.timeout <= 0:
                raise PocError("timeoutは正の秒数を指定してください")
            value = transcribe(args.wav, args.model, args.whisper_cli, args.timeout, args.gpu)
            result = {"recognized": bool(value), "characters": len(value)}
            if args.show_text:
                result["text"] = value
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except PocError as error:
        print(f"エラー: {error}", file=sys.stderr)
        return 1
    except KeyboardInterrupt:
        print("停止しました", file=sys.stderr)
        return 130


if __name__ == "__main__":
    sys.exit(main())
