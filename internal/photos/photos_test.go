package photos

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"golang.org/x/image/webp"
)

// marked is w×h, white, with a red pixel top left and a blue pixel top right.
func marked(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	img.Set(w-1, 0, color.RGBA{0, 0, 255, 255})
	return img
}

func TestOrient(t *testing.T) {
	red, blue := color.RGBA{255, 0, 0, 255}, color.RGBA{0, 0, 255, 255}
	// Where red and blue end up, on a 3×2 picture, for each orientation.
	want := map[int][2]image.Point{
		1: {{0, 0}, {2, 0}},
		2: {{2, 0}, {0, 0}},
		3: {{2, 1}, {0, 1}},
		4: {{0, 1}, {2, 1}},
		5: {{0, 0}, {0, 2}},
		6: {{1, 0}, {1, 2}},
		7: {{1, 2}, {1, 0}},
		8: {{0, 2}, {0, 0}},
	}
	for o, pts := range want {
		got := orient(marked(3, 2), o)
		if got.RGBAAt(pts[0].X, pts[0].Y) != red || got.RGBAAt(pts[1].X, pts[1].Y) != blue {
			t.Errorf("orientation %d: red/blue not at %v (size %v)", o, pts, got.Bounds().Size())
		}
	}
}

// phoneJPEG is a JPEG with an EXIF block holding orientation o and a GPS note.
func phoneJPEG(t *testing.T, img image.Image, o uint16) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	tiff := []byte("II\x2a\x00\x08\x00\x00\x00\x01\x00\x12\x01\x03\x00\x01\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00")
	binary.LittleEndian.PutUint16(tiff[18:], o)
	body := append(append([]byte("Exif\x00\x00"), tiff...), "GPS 32.7157 N 117.1611 W"...)
	seg := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(seg[2:], uint16(len(body)+2))
	out := append([]byte{0xFF, 0xD8}, seg...)
	out = append(out, body...)
	return append(out, buf.Bytes()[2:]...)
}

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestProcessPhonePhoto(t *testing.T) {
	// A 3000×1000 sensor image the phone says to turn a quarter clockwise.
	sensor := marked(3000, 1000)
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			sensor.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	in := phoneJPEG(t, sensor, 6)
	res, err := Process(in, true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(res.Full, []byte("GPS")) || bytes.Contains(res.Full, []byte("Exif")) {
		t.Fatal("metadata should not carry over")
	}
	full, small := decode(t, res.Full), decode(t, res.Small)
	if s := full.Bounds().Size(); s != image.Pt(667, 2000) {
		t.Fatalf("full size %v, want turned and shrunk to 667×2000", s)
	}
	if s := small.Bounds().Size(); s.Y != SmallMax {
		t.Fatalf("small size %v", s)
	}
	// The red corner was top left on the sensor, so it is top right once turned.
	if r, g, _, _ := full.At(660, 5).RGBA(); r>>8 < 200 || g>>8 > 80 {
		t.Fatalf("top right should be red, got %v", full.At(660, 5))
	}
}

func TestProcessKeepsTransparency(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	img.Set(5, 5, color.NRGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	res, err := Process(buf.Bytes(), false)
	if err != nil {
		t.Fatal(err)
	}
	if res.Small != nil {
		t.Fatal("no small version was asked for")
	}
	want := ".png"
	if WebP() {
		want = ".webp"
	}
	if res.Ext != want {
		t.Fatalf("ext %s, want %s", res.Ext, want)
	}
	if _, _, _, a := decode(t, res.Full).At(0, 0).RGBA(); a != 0 {
		t.Fatal("transparent corner should stay transparent")
	}
}

func TestProcessRefusesHugePictures(t *testing.T) {
	// A PNG header claiming 20000×20000 is refused before any decoding.
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewGray(image.Rect(0, 0, 1, 1)))
	b := buf.Bytes()
	binary.BigEndian.PutUint32(b[16:], 20000)
	binary.BigEndian.PutUint32(b[20:], 20000)
	binary.BigEndian.PutUint32(b[29:], crc32.ChecksumIEEE(b[12:29]))
	if _, err := Process(b, false); err != ErrTooLarge {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestWebPOutput(t *testing.T) {
	if !WebP() {
		t.Skip("libwebp is not installed")
	}
	res, err := Process(phoneJPEG(t, marked(800, 600), 1), true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Ext != ".webp" {
		t.Fatalf("ext %s", res.Ext)
	}
	img, err := webp.Decode(bytes.NewReader(res.Full))
	if err != nil || img.Bounds().Size() != image.Pt(800, 600) {
		t.Fatalf("webp decode: %v", err)
	}
}

func TestNames(t *testing.T) {
	if SmallName("ab12.webp") != "ab12-sm.webp" {
		t.Fatal(SmallName("ab12.webp"))
	}
}
