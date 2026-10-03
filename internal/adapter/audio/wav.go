// Package audio は WAV の読み書き、マイク入力、スピーカー再生を扱う。
package audio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// PCM は16bitモノラル音声。
type PCM struct {
	SampleRate int
	Samples    []int16
}

// DecodeWAV は無圧縮・16bit・モノラルの WAV だけを受け付ける。
func DecodeWAV(data []byte) (PCM, error) {
	r := bytes.NewReader(data)
	var header struct {
		Riff [4]byte
		Size uint32
		Wave [4]byte
	}
	if err := binary.Read(r, binary.LittleEndian, &header); err != nil || string(header.Riff[:]) != "RIFF" ||
		string(header.Wave[:]) != "WAVE" {
		return PCM{}, errors.New("WAV形式ではありません")
	}
	var format struct {
		AudioFormat, Channels uint16
		SampleRate, ByteRate  uint32
		BlockAlign, Bits      uint16
	}
	haveFormat := false
	for {
		var chunk struct {
			ID   [4]byte
			Size uint32
		}
		if err := binary.Read(r, binary.LittleEndian, &chunk); err != nil {
			return PCM{}, errors.New("WAVに音声データがありません")
		}
		switch string(chunk.ID[:]) {
		case "fmt ":
			if chunk.Size < 16 {
				return PCM{}, errors.New("WAVの形式情報が不正です")
			}
			if err := binary.Read(r, binary.LittleEndian, &format); err != nil {
				return PCM{}, errors.New("WAVの形式情報が不正です")
			}
			if _, err := r.Seek(int64(chunk.Size-16+chunk.Size%2), io.SeekCurrent); err != nil {
				return PCM{}, err
			}
			haveFormat = true
		case "data":
			if !haveFormat || format.AudioFormat != 1 || format.Channels != 1 || format.Bits != 16 {
				return PCM{}, errors.New("WAVは16bit・モノラルのPCMにしてください")
			}
			if int64(chunk.Size) > int64(r.Len()) || chunk.Size%2 != 0 {
				return PCM{}, errors.New("WAVの音声データが途中で切れています")
			}
			samples := make([]int16, chunk.Size/2)
			if err := binary.Read(r, binary.LittleEndian, samples); err != nil {
				return PCM{}, err
			}
			if len(samples) == 0 {
				return PCM{}, errors.New("WAVに音声フレームがありません")
			}
			return PCM{SampleRate: int(format.SampleRate), Samples: samples}, nil
		default:
			if _, err := r.Seek(int64(chunk.Size+chunk.Size%2), io.SeekCurrent); err != nil {
				return PCM{}, err
			}
		}
	}
}

// ReadRequestWAV は検出・認識に使う16kHzの WAV を読む。
func ReadRequestWAV(path string) ([]int16, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("WAVを読み込めません: %w", err)
	}
	pcm, err := DecodeWAV(data)
	if err != nil {
		return nil, err
	}
	if pcm.SampleRate != 16000 {
		return nil, errors.New("WAVは16kHz・モノラル・16bit PCMで用意してください")
	}
	return pcm.Samples, nil
}

func EncodeWAV(pcm PCM) []byte {
	var b bytes.Buffer
	size := uint32(len(pcm.Samples) * 2)
	b.WriteString("RIFF")
	binary.Write(&b, binary.LittleEndian, 36+size)
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(pcm.SampleRate),
		uint32(pcm.SampleRate * 2), uint16(2), uint16(16)} {
		binary.Write(&b, binary.LittleEndian, v) // bytes.Buffer への書き込みは失敗しない
	}
	b.WriteString("data")
	binary.Write(&b, binary.LittleEndian, size)
	binary.Write(&b, binary.LittleEndian, pcm.Samples)
	return b.Bytes()
}
