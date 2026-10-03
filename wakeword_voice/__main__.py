import argparse
import json
import sys
from pathlib import Path

from .poc import PocError, detect_wake, transcribe, validate_wav


def main():
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
    stt.add_argument("--show-text", action="store_true", help="文字起こしを標準出力に表示する")
    args = parser.parse_args()
    try:
        if args.command == "check-wav":
            result = validate_wav(args.wav)
        elif args.command == "detect-wake":
            result = detect_wake(args.wav, args.model, args.threshold)
        else:
            if args.timeout <= 0:
                raise PocError("timeoutは正の秒数を指定してください")
            value = transcribe(args.wav, args.model, args.whisper_cli, args.timeout)
            result = {"recognized": bool(value), "characters": len(value)}
            if args.show_text:
                result["text"] = value
        print(json.dumps(result, ensure_ascii=False))
        return 0
    except PocError as error:
        print(f"エラー: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
