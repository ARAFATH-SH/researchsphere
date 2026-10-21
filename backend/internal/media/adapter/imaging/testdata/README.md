# Image fixtures

Small images for the imaging and media tests. Every fixture except the WebP ones is
written by `fixtures_test.go`:

    go test ./internal/media/adapter/imaging -run TestWriteFixtures -update

| File | What it is |
| --- | --- |
| `exif.jpg` | 64x48 JPEG with an EXIF segment holding the text `SECRET-EXIF-MARKER` and a GPS IFD |
| `rotated.jpg` | Stored 64x32 (left red, right blue) with EXIF Orientation 6; upright it is 32x64, red on top |
| `alpha.png` | 30x20 PNG, left half transparent, right half green |
| `huge.png` | 9000x1 PNG: a header over the 8000-pixel limit in 92 bytes |
| `notimage.jpg` | A text file named `.jpg` |
| `still.webp` | 48x36 lossy WebP, one colour |
| `animated.webp` | 16x16 WebP with two frames (VP8X animation flag set) |

Go cannot write WebP, so the two WebP files were made with Pillow 12.1:

    from PIL import Image
    Image.new("RGB", (48, 36), (0, 128, 255)).save("still.webp", "WEBP", quality=80)
    frames = [Image.new("RGB", (16, 16), c) for c in [(255, 0, 0), (0, 0, 255)]]
    frames[0].save("animated.webp", "WEBP", save_all=True, append_images=frames[1:], duration=100, loop=0)
