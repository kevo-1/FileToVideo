package services

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/reedsolomon"

	"github.com/kevo-1/FileToVideo/internal/repository"
)

const (
	FrameWidth  = 3840
	FrameHeight = 2160
	FPS         = 30

	BlockSize = 4

	DataShards       = 10
	ParityShards     = 3
	TotalShards      = DataShards + ParityShards
	ShardPayloadSize = 1024
	ShardWireSize    = 4 + ShardPayloadSize

	StripePayloadSize = DataShards * ShardPayloadSize
	StripeWireSize    = TotalShards * ShardWireSize

	BlocksPerRow = FrameWidth / BlockSize
	BlocksPerCol = FrameHeight / BlockSize
	BitsPerFrame = BlocksPerRow * BlocksPerCol

	BlockThreshold = 128
)

// Header layout (96 bytes).
//
//	offset  size  field
//	0       4     magic "F2V1"
//	4       1     version
//	5       3     reserved
//	8       8     payloadSize  (uint64, header + original file bytes)
//	16      8     originalSize (uint64, original file bytes only)
//	24      8     createdUnix  (int64)
//	32      16    ext          (null padding, (e.g. .mp3))
//	48      32    sha256 of the original file
//	80      16    reserved
const (
	HeaderSize   = 96
	HeaderMagic  = "F2V1"
	FormatVer    = 1
	ExtFieldSize = 16
)

// reconstruct the original file from a video.
type FileMeta struct {
	OriginalExt  string
	OriginalSize uint64
	CreatedAt    time.Time
	Checksum     [32]byte
}

func ProcessFile(ctx context.Context, tempFile *repository.TempFile) (string, error) {
	data, err := tempFile.Read()
	if err != nil {
		return "", fmt.Errorf("processing: reading temp file: %w", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("processing: uploaded file is empty")
	}

	meta := FileMeta{
		OriginalExt:  filepath.Ext(tempFile.FileName),
		OriginalSize: uint64(len(data)),
		CreatedAt:    time.Now().UTC(),
		Checksum:     sha256.Sum256(data),
	}

	payload := make([]byte, 0, HeaderSize+len(data))
	payload = append(payload, BuildHeader(meta, uint64(HeaderSize+len(data)))...)
	payload = append(payload, data...)

	wire, err := ECCEncode(payload)
	if err != nil {
		return "", fmt.Errorf("processing: ecc encode: %w", err)
	}

	frameDir := tempFile.Path() + "-frames"
	if err := os.MkdirAll(frameDir, 0o755); err != nil {
		return "", fmt.Errorf("processing: creating frame dir: %w", err)
	}
	defer os.RemoveAll(frameDir)

	frameCount, err := WriteFrames(wire, frameDir)
	if err != nil {
		return "", fmt.Errorf("processing: writing frames: %w", err)
	}

	videoPath := tempFile.Path() + ".mkv"
	if err := AssembleVideo(ctx, frameDir, videoPath); err != nil {
		return "", fmt.Errorf("processing: assembling video: %w", err)
	}

	fmt.Printf("encoded %d bytes into %d frames -> %s\n", len(data), frameCount, videoPath)
	return videoPath, nil
}

func RevertVideo(ctx context.Context, videoPath, outDir string) (string, error) {
	frameDir := videoPath + "-frames"
	if err := os.MkdirAll(frameDir, 0o755); err != nil {
		return "", fmt.Errorf("reverting: creating frame dir: %w", err)
	}
	defer os.RemoveAll(frameDir)

	if err := ExtractFrames(ctx, videoPath, frameDir); err != nil {
		return "", fmt.Errorf("reverting: extracting frames: %w", err)
	}

	wire, err := ReadFrames(frameDir)
	if err != nil {
		return "", fmt.Errorf("reverting: reading frames: %w", err)
	}

	payload, err := ECCDecode(wire)
	if err != nil {
		return "", fmt.Errorf("reverting: ecc decode: %w", err)
	}

	meta, _, err := ParseHeader(payload[:HeaderSize])
	if err != nil {
		return "", fmt.Errorf("reverting: %w", err)
	}

	body := payload[HeaderSize:]
	if uint64(len(body)) < meta.OriginalSize {
		return "", fmt.Errorf("reverting: truncated payload: have %d bytes, header says %d",
			len(body), meta.OriginalSize)
	}

	body = body[:meta.OriginalSize]

	if got := sha256.Sum256(body); got != meta.Checksum {
		return "", fmt.Errorf("reverting: checksum mismatch, recovered data is corrupt")
	}

	outPath := filepath.Join(outDir, "recovered"+meta.OriginalExt)
	if err := os.WriteFile(outPath, body, 0o644); err != nil {
		return "", fmt.Errorf("reverting: writing recovered file: %w", err)
	}
	return outPath, nil
}

// serialise meta into the fixed-size header block.
func BuildHeader(meta FileMeta, payloadSize uint64) []byte {
	h := make([]byte, HeaderSize)

	copy(h[0:4], HeaderMagic)
	h[4] = FormatVer

	binary.BigEndian.PutUint64(h[8:16], payloadSize)
	binary.BigEndian.PutUint64(h[16:24], meta.OriginalSize)
	binary.BigEndian.PutUint64(h[24:32], uint64(meta.CreatedAt.Unix()))

	ext := meta.OriginalExt
	if len(ext) > ExtFieldSize {
		ext = ext[:ExtFieldSize]
	}
	copy(h[32:48], ext)
	copy(h[48:80], meta.Checksum[:])

	return h
}

// read a header block and return the metadata (header+file bytes).
func ParseHeader(h []byte) (FileMeta, uint64, error) {
	var meta FileMeta

	if len(h) < HeaderSize {
		return meta, 0, fmt.Errorf("parsing header: need %d bytes, got %d", HeaderSize, len(h))
	}
	if string(h[0:4]) != HeaderMagic {
		return meta, 0, fmt.Errorf("parsing header: bad magic bytes, not a FileToVideo stream")
	}
	if h[4] != FormatVer {
		return meta, 0, fmt.Errorf("parsing header: unsupported format version %d", h[4])
	}

	payloadSize := binary.BigEndian.Uint64(h[8:16])
	meta.OriginalSize = binary.BigEndian.Uint64(h[16:24])
	meta.CreatedAt = time.Unix(int64(binary.BigEndian.Uint64(h[24:32])), 0).UTC()
	meta.OriginalExt = strings.TrimRight(string(h[32:48]), "\x00")
	copy(meta.Checksum[:], h[48:80])

	if payloadSize < HeaderSize {
		return meta, 0, fmt.Errorf("parsing header: payload size %d smaller than header", payloadSize)
	}
	return meta, payloadSize, nil
}

// split payload into stripes, and prefixe every shard with a CRC32 so the decoder can tell which shards were damaged.
func ECCEncode(payload []byte) ([]byte, error) {
	enc, err := reedsolomon.New(DataShards, ParityShards)
	if err != nil {
		return nil, fmt.Errorf("building encoder: %w", err)
	}

	stripeCount := (len(payload) + StripePayloadSize - 1) / StripePayloadSize
	padded := make([]byte, stripeCount*StripePayloadSize)
	copy(padded, payload)

	out := make([]byte, 0, stripeCount*StripeWireSize)
	for i := 0; i < stripeCount; i++ {
		chunk := padded[i*StripePayloadSize : (i+1)*StripePayloadSize]
		stripe, err := encodeStripe(enc, chunk)
		if err != nil {
			return nil, fmt.Errorf("encoding stripe %d: %w", i, err)
		}
		out = append(out, stripe...)
	}
	return out, nil
}

func ECCDecode(wire []byte) ([]byte, error) {
	enc, err := reedsolomon.New(DataShards, ParityShards)
	if err != nil {
		return nil, fmt.Errorf("building encoder: %w", err)
	}
	if len(wire) < StripeWireSize {
		return nil, fmt.Errorf("stream too short: %d bytes", len(wire))
	}

	first, err := decodeStripe(enc, wire[:StripeWireSize])
	if err != nil {
		return nil, fmt.Errorf("decoding header stripe: %w", err)
	}

	_, payloadSize, err := ParseHeader(first[:HeaderSize])
	if err != nil {
		return nil, err
	}

	stripeCount := int((payloadSize + StripePayloadSize - 1) / StripePayloadSize)
	if have := len(wire) / StripeWireSize; have < stripeCount {
		return nil, fmt.Errorf("stream truncated: need %d stripes, have %d", stripeCount, have)
	}

	out := make([]byte, 0, stripeCount*StripePayloadSize)
	out = append(out, first...)
	for i := 1; i < stripeCount; i++ {
		chunk := wire[i*StripeWireSize : (i+1)*StripeWireSize]
		stripe, err := decodeStripe(enc, chunk)
		if err != nil {
			return nil, fmt.Errorf("decoding stripe %d: %w", i, err)
		}
		out = append(out, stripe...)
	}

	return out[:payloadSize], nil
}

func encodeStripe(enc reedsolomon.Encoder, payload []byte) ([]byte, error) {
	shards := make([][]byte, TotalShards)
	for i := range shards {
		shards[i] = make([]byte, ShardPayloadSize)
	}
	for i := 0; i < DataShards; i++ {
		copy(shards[i], payload[i*ShardPayloadSize:(i+1)*ShardPayloadSize])
	}
	if err := enc.Encode(shards); err != nil {
		return nil, err
	}

	out := make([]byte, 0, StripeWireSize)
	for _, s := range shards {
		var crcBuf [4]byte
		binary.BigEndian.PutUint32(crcBuf[:], crc32.ChecksumIEEE(s))
		out = append(out, crcBuf[:]...)
		out = append(out, s...)
	}
	return out, nil
}

func decodeStripe(enc reedsolomon.Encoder, wire []byte) ([]byte, error) {
	shards := make([][]byte, TotalShards)
	intact := 0

	for i := range shards {
		rec := wire[i*ShardWireSize : (i+1)*ShardWireSize]
		want := binary.BigEndian.Uint32(rec[:4])
		body := rec[4:]

		if crc32.ChecksumIEEE(body) != want {
			continue
		}
		s := make([]byte, ShardPayloadSize)
		copy(s, body)
		shards[i] = s
		intact++
	}

	if intact < DataShards {
		return nil, fmt.Errorf("unrecoverable: only %d of %d shards survived (need %d)",
			intact, TotalShards, DataShards)
	}
	if err := enc.ReconstructData(shards); err != nil {
		return nil, fmt.Errorf("reconstructing: %w", err)
	}

	out := make([]byte, 0, StripePayloadSize)
	for i := 0; i < DataShards; i++ {
		out = append(out, shards[i]...)
	}
	return out, nil
}

// build the wire bytes into grayscale PNG frames in dir
func WriteFrames(wire []byte, dir string) (int, error) {
	totalBits := len(wire) * 8
	frameCount := (totalBits + BitsPerFrame - 1) / BitsPerFrame

	for f := 0; f < frameCount; f++ {
		startBit := f * BitsPerFrame
		bitCount := totalBits - startBit
		if bitCount > BitsPerFrame {
			bitCount = BitsPerFrame
		}

		img := renderFrame(wire, startBit, bitCount)
		path := filepath.Join(dir, fmt.Sprintf("frame_%06d.png", f+1))
		if err := writePNG(img, path); err != nil {
			return 0, fmt.Errorf("writing frame %d: %w", f+1, err)
		}
	}
	return frameCount, nil
}

// decode every PNG in dir back into the wire byte stream
func ReadFrames(dir string) ([]byte, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("listing frames: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".png") {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("no frames found in %s", dir)
	}
	sort.Strings(names)

	totalBits := len(names) * BitsPerFrame
	out := make([]byte, (totalBits+7)/8)

	bitIdx := 0
	for _, name := range names {
		img, err := readGrayPNG(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}
		if img.Bounds().Dx() != FrameWidth || img.Bounds().Dy() != FrameHeight {
			return nil, fmt.Errorf("frame %s is %dx%d, expected %dx%d",
				name, img.Bounds().Dx(), img.Bounds().Dy(), FrameWidth, FrameHeight)
		}
		decodeFrame(img, out, bitIdx)
		bitIdx += BitsPerFrame
	}
	return out, nil
}

func renderFrame(wire []byte, startBit, bitCount int) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, FrameWidth, FrameHeight))
	for i := 0; i < bitCount; i++ {
		if getBit(wire, startBit+i) == 0 {
			continue
		}
		x0 := (i % BlocksPerRow) * BlockSize
		y0 := (i / BlocksPerRow) * BlockSize
		for y := y0; y < y0+BlockSize; y++ {
			row := img.Pix[y*img.Stride:]
			for x := x0; x < x0+BlockSize; x++ {
				row[x] = 255
			}
		}
	}
	return img
}

// average each BlockSize x BlockSize square.
func decodeFrame(img *image.Gray, out []byte, startBit int) {
	for i := 0; i < BitsPerFrame; i++ {
		bitIdx := startBit + i
		if bitIdx >= len(out)*8 {
			return
		}
		x0 := (i % BlocksPerRow) * BlockSize
		y0 := (i / BlocksPerRow) * BlockSize

		sum := 0
		for y := y0; y < y0+BlockSize; y++ {
			row := img.Pix[y*img.Stride:]
			for x := x0; x < x0+BlockSize; x++ {
				sum += int(row[x])
			}
		}
		if sum/(BlockSize*BlockSize) >= BlockThreshold {
			setBit(out, bitIdx)
		}
	}
}

func getBit(data []byte, i int) byte {
	return (data[i/8] >> (7 - uint(i%8))) & 1
}

func setBit(data []byte, i int) {
	data[i/8] |= 1 << (7 - uint(i%8))
}

func writePNG(img *image.Gray, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	return f.Close()
}

func readGrayPNG(path string) (*image.Gray, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	if g, ok := img.(*image.Gray); ok {
		return g, nil
	}

	b := img.Bounds()
	g := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g.Set(x, y, img.At(x, y))
		}
	}
	return g, nil
}

// assemble the PNG frames into a lossless FFV1 video in an MKV container.
func AssembleVideo(ctx context.Context, frameDir, outPath string) error {
	args := []string{
		"-y",
		"-framerate", fmt.Sprint(FPS),
		"-i", filepath.Join(frameDir, "frame_%06d.png"),
		"-c:v", "ffv1",
		"-pix_fmt", "gray",
		outPath,
	}
	return runFFmpeg(ctx, args)
}

// pull every frame out of a video as a grayscale PNG.
func ExtractFrames(ctx context.Context, videoPath, frameDir string) error {
	args := []string{
		"-y",
		"-i", videoPath,
		"-pix_fmt", "gray",
		"-start_number", "1",
		filepath.Join(frameDir, "frame_%06d.png"),
	}
	return runFFmpeg(ctx, args)
}

func runFFmpeg(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if len(msg) > 2000 {
			msg = msg[len(msg)-2000:]
		}
		return fmt.Errorf("ffmpeg failed: %w: %s", err, msg)
	}
	return nil
}
