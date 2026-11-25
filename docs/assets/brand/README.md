# pergent logo guide

## 1. The logo

**The notch.** Two lines of code. The second line dips at the spot the review flags: an inline finding.

- One line weight, round ends, straight lines only. No circles, no curves, no colour.
- The wordmark is drawn from the same line, as square lowercase letters. It is not a font, so no font licence is needed.

| File | Use it for |
|---|---|
| `pergent-horizontal.svg` | Default logo: README header, website header, slides |
| `pergent-stacked.svg` | Square-ish spaces: posters, title slides, stickers |
| `pergent-mark.svg` | The mark alone, 48 px and larger |
| `pergent-mark-small.svg` | The mark at 16-47 px (thicker lines, 2 px at 16 px) |
| `pergent-wordmark.svg` | The name alone, when the mark is already on the page |
| `pergent-sh-horizontal.svg`, `pergent-sh-wordmark.svg` | The same for the pergent.sh service. The period is a short pill, not a round dot |
| `*-white.svg`, `pergent-mark-reversed.svg` | White on dark backgrounds. The reversed mark is 1 unit thinner per side, because white on black looks heavier |
| `pergent-mark-app-icon.svg` | White mark on a black rounded square |
| `web/` | `favicon.ico`, `favicon.svg`, PNG icons, `site.webmanifest`, `head-snippet.html` |
| `*-stroke.svg` | Editable source (stroked lines). Edit these, not the masters |

The masters are filled outlines, checked against the stroked source: 0 different pixels at 1024 px.
The outlines overlap where two lines meet; they use one winding direction, so they render as one solid shape.
For a plotter, vinyl cutter or embroidery file, merge them once in a vector editor ("Union").

## 2. Construction

- Canvas 256 × 256. Line weight 24 (12 each side of the centre line).
- Top line from x 40 to 168 at y 74. Second line from x 40 to 216 at y 122.
- The notch: arms at 60°, 52 units deep, starting at x 122.
- Wordmark: x-height 128, same 24 line, square corners with round outer joins, 30 units between letters.
- Horizontal lockup: mark at 1.3×, centred on the x-height, 64 units before the "p".

## 3. Clear space

Keep empty space of at least **2 line weights** on every side (48 units at master size).
On the horizontal lockup, that is about the height of the notch.

## 4. Minimum size

| Version | Minimum |
|---|---|
| Mark | 48 px; below that use `pergent-mark-small.svg`, down to 16 px |
| Horizontal lockup | 120 px wide |
| Stacked lockup | 80 px wide |

At 16 px the small cut is soft but still reads as two lines and a notch. Use 32 px or larger where you can.

## 5. Colour

| | HEX | RGB |
|---|---|---|
| Black | `#000000` | 0 0 0 |
| White | `#FFFFFF` | 255 255 255 |

Black on light backgrounds, white on dark backgrounds. Nothing else.

## 6. Do not

- Add colour, gradients, shadows or outlines.
- Make the notch round, or curve any line.
- Give the mark and the wordmark different line weights (the 1.3× and 1.6× lockup scale is the only change).
- Stretch, rotate or re-space the letters.
- Put the mark inside a circle or badge (a platform's own round avatar crop is fine).
- Set "pergent" in a font next to the mark instead of using the drawn wordmark.

## 7. Rebuild

`build_pergent.py` makes every master and stroke source from the line coordinates.
Change a coordinate there and run `python3 build_pergent.py <out-dir>`.
The `-black`, `-white`, PNG and `web/` files were exported from the masters; export them again after a change.

A trademark search was not done. Do one before wide use.
