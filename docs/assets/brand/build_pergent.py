#!/usr/bin/env python3
"""Build the pergent logo kit masters: mark, small cut, wordmark, lockups.

Every shape is a polyline with round caps and round joins. The script writes
two versions of each file:
  *-stroke.svg   editable source (stroked polylines)
  *.svg          master (strokes expanded to filled outlines, nonzero fill,
                 all subpaths wound the same way, so overlaps stay solid)
"""
import math
import os
import sys

OUT = sys.argv[1] if len(sys.argv) > 1 else "kit"
os.makedirs(OUT, exist_ok=True)


def f(v):
    return f"{round(v, 2):g}"


def outline(poly, r):
    """Filled outline of an open polyline with round caps and joins."""
    pts = [poly[0]]
    for p in poly[1:]:  # drop repeated and collinear points
        if p != pts[-1]:
            pts.append(p)
    clean = [pts[0]]
    for i in range(1, len(pts) - 1):
        a, b, c = clean[-1], pts[i], pts[i + 1]
        cr = (b[0] - a[0]) * (c[1] - b[1]) - (b[1] - a[1]) * (c[0] - b[0])
        if abs(cr) > 1e-9:
            clean.append(b)
    clean.append(pts[-1])
    pts = clean
    dirs, norms = [], []
    for a, b in zip(pts, pts[1:]):
        L = math.hypot(b[0] - a[0], b[1] - a[1])
        d = ((b[0] - a[0]) / L, (b[1] - a[1]) / L)
        dirs.append(d)
        norms.append((-d[1], d[0]))

    def side(sign, seq_pts, seq_dirs, seq_norms):
        """Offset one side; returns list of path commands after the first point."""
        cmds = []
        for k in range(1, len(seq_pts) - 1):
            P = seq_pts[k]
            n1 = (seq_norms[k - 1][0] * sign, seq_norms[k - 1][1] * sign)
            n2 = (seq_norms[k][0] * sign, seq_norms[k][1] * sign)
            d1, d2 = seq_dirs[k - 1], seq_dirs[k]
            turn = d1[0] * d2[1] - d1[1] * d2[0]
            # this side is convex when the path turns away from it
            nd = n1[0] * d2[0] + n1[1] * d2[1]
            if nd < 0:  # convex: round join
                a = (P[0] + r * n1[0], P[1] + r * n1[1])
                b = (P[0] + r * n2[0], P[1] + r * n2[1])
                cmds.append(f"L{f(a[0])} {f(a[1])}")
                sweep = 1 if (n1[0] * n2[1] - n1[1] * n2[0]) > 0 else 0
                cmds.append(f"A{f(r)} {f(r)} 0 0 {sweep} {f(b[0])} {f(b[1])}")
            else:  # concave: mitre point
                dot = n1[0] * n2[0] + n1[1] * n2[1]
                m = (P[0] + r * (n1[0] + n2[0]) / (1 + dot), P[1] + r * (n1[1] + n2[1]) / (1 + dot))
                cmds.append(f"L{f(m[0])} {f(m[1])}")
            _ = turn
        last = seq_pts[-1]
        nl = (seq_norms[-1][0] * sign, seq_norms[-1][1] * sign)
        e = (last[0] + r * nl[0], last[1] + r * nl[1])
        cmds.append(f"L{f(e[0])} {f(e[1])}")
        return cmds

    n0 = norms[0]
    start = (pts[0][0] + r * n0[0], pts[0][1] + r * n0[1])
    path = [f"M{f(start[0])} {f(start[1])}"]
    path += side(1, pts, dirs, norms)
    # end cap: from +n to -n around the forward direction
    last, nl, dl = pts[-1], norms[-1], dirs[-1]
    e2 = (last[0] - r * nl[0], last[1] - r * nl[1])
    sweep = 1 if (nl[0] * dl[1] - nl[1] * dl[0]) > 0 else 0
    path.append(f"A{f(r)} {f(r)} 0 0 {sweep} {f(e2[0])} {f(e2[1])}")
    # back along the other side
    rp = pts[::-1]
    rd = [(-d[0], -d[1]) for d in dirs[::-1]]
    rn = [(-d[1], d[0]) for d in rd]
    path += side(1, rp, rd, rn)
    # start cap
    n0b = rn[-1]
    d0b = rd[-1]
    sweep = 1 if (n0b[0] * d0b[1] - n0b[1] * d0b[0]) > 0 else 0
    path.append(f"A{f(r)} {f(r)} 0 0 {sweep} {f(start[0])} {f(start[1])}")
    path.append("Z")
    return "".join(path)


def bbox(polys, r):
    xs = [p[0] for pl in polys for p in pl]
    ys = [p[1] for pl in polys for p in pl]
    return min(xs) - r, min(ys) - r, max(xs) + r, max(ys) + r


def move(polys, dx, dy, s=1.0):
    return [[(x * s + dx, y * s + dy) for x, y in pl] for pl in polys]


def write(name, polys, r, viewbox, title="pergent", color="#000", extra=()):
    """polys share radius r; extra = [(polys, r), ...] for groups with another stroke."""
    groups = [(polys, r)] + list(extra)
    vb = " ".join(f(v) for v in viewbox)
    head = (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{vb}" role="img" aria-label="{title}">'
            f"<title>{title}</title>")
    with open(os.path.join(OUT, f"{name}-stroke.svg"), "w") as fh:
        fh.write(head)
        for ps, rr in groups:
            d = " ".join("M" + " L".join(f"{f(x)} {f(y)}" for x, y in pl) for pl in ps)
            fh.write(f'<path fill="none" stroke="{color}" stroke-width="{f(2 * rr)}" '
                     f'stroke-linecap="round" stroke-linejoin="round" d="{d}"/>')
        fh.write("</svg>\n")
    fill_d = "".join(outline(pl, rr) for ps, rr in groups for pl in ps)
    with open(os.path.join(OUT, f"{name}.svg"), "w") as fh:
        fh.write(head + f'<path fill="{color}" fill-rule="nonzero" d="{fill_d}"/></svg>\n')


R = 12  # half stroke: stroke is 24 units on a 256 canvas

# ---- mark: two lines of code, the second one dips at the finding (60 degree arms) ----
DX = 52 / math.tan(math.radians(60))
MARK = [
    [(40, 74), (168, 74)],
    [(40, 122), (122, 122), (122 + DX, 174), (122 + 2 * DX, 122), (216, 122)],
]
write("pergent-mark", MARK, R, (0, 0, 256, 256))

# ---- small cut for 16-32 px: stroke 32 (2 px at 16 px), lines on the pixel grid ----
RS = 16
SDX = 48 / math.tan(math.radians(60))
SMALL = [
    [(48, 64), (160, 64)],
    [(48, 128), (120, 128), (120 + SDX, 176), (120 + 2 * SDX, 128), (208, 128)],
]
write("pergent-mark-small", SMALL, RS, (0, 0, 256, 256))

# ---- reversed (white on dark) is thinned 1 unit per side against irradiation ----
write("pergent-mark-reversed", MARK, R - 1, (0, 0, 256, 256), color="#fff")

# ---- wordmark: square monoline lowercase, same stroke as the mark ----
YT, YB, YD, YA, YC = -116, -12, 44, -148, -64  # x-height, baseline, descender, ascender, e-bar
W = 64
GLYPHS = {
    "p": ([[(0, YD), (0, YT)], [(0, YT), (W, YT), (W, YB), (0, YB)]], W),
    "e": ([[(0, YC), (W, YC), (W, YT), (0, YT)], [(0, YT), (0, YB), (W, YB)]], W),
    "r": ([[(0, YB), (0, YT), (44, YT)]], 44),
    "g": ([[(W, YB), (0, YB), (0, YT), (W, YT)], [(W, YT), (W, YD), (0, YD)]], W),
    "n": ([[(0, YB), (0, YT), (W, YT), (W, YB)]], W),
    "t": ([[(16, YA), (16, YB), (56, YB)], [(0, YT), (52, YT)]], 56),
    "s": ([[(W, YT), (0, YT), (0, YC), (W, YC), (W, YB), (0, YB)]], W),
    "h": ([[(0, YA), (0, YB)], [(0, YT), (W, YT), (W, YB)]], W),
    ".": ([[(0, YB), (8, YB)]], 8),  # a short pill, not a round dot
}
GAP = 30          # white space between letters, outer edge to outer edge
KERN = {"r": -10, ".": -6}  # the r's open lower right needs less space after it


def set_word(word):
    polys, x = [], 0
    for ch in word:
        gp, w = GLYPHS[ch]
        polys += move(gp, x, 0)
        x += w + 2 * R + GAP + KERN.get(ch, 0)
    return polys


WORD = set_word("pergent")
bx0, by0, bx1, by1 = bbox(WORD, R)
PAD = 24
write("pergent-wordmark", move(WORD, PAD - bx0, PAD - by0), R,
      (0, 0, bx1 - bx0 + 2 * PAD, by1 - by0 + 2 * PAD))

# ---- horizontal lockup: mark 1.3x (stroke too), centred on the x-height, 64 units before the word ----
mx0, my0, mx1, my1 = bbox(MARK, R)
SH = 1.3
xh_mid = (YT - R + YB + R) / 2


def horizontal(name, word):
    w0, wy0, w1, wy1 = bbox(word, R)
    mark_h = move(MARK, -mx0 * SH, xh_mid - (my0 + my1) / 2 * SH, SH)
    word_h = move(word, (mx1 - mx0) * SH + 64 - w0, 0)
    hy0 = min(wy0, xh_mid - (my1 - my0) * SH / 2)
    hx1 = (mx1 - mx0) * SH + 64 + (w1 - w0)
    hy1 = max(wy1, xh_mid + (my1 - my0) * SH / 2)
    write(name, move(word_h, PAD, PAD - hy0), R,
          (0, 0, hx1 + 2 * PAD, hy1 - hy0 + 2 * PAD),
          extra=[(move(mark_h, PAD, PAD - hy0), R * SH)])


horizontal("pergent-horizontal", WORD)

# ---- pergent.sh: the service's own wordmark and lockup ----
WORD_SH = set_word("pergent.sh")
sx0_, sy0_, sx1_, sy1_ = bbox(WORD_SH, R)
write("pergent-sh-wordmark", move(WORD_SH, PAD - sx0_, PAD - sy0_), R,
      (0, 0, sx1_ - sx0_ + 2 * PAD, sy1_ - sy0_ + 2 * PAD))
horizontal("pergent-sh-horizontal", WORD_SH)

# ---- stacked lockup: mark scaled 1.6x (stroke too) and centred 48 units above the ascender ----
S = 1.6
ww = bx1 - bx0
mw = (mx1 - mx0) * S
mark_s = move(MARK, (ww - mw) / 2 - mx0 * S, by0 - 48 - my1 * S, S)
word_s = move(WORD, -bx0, 0)
sx0, sy0 = min(0, (ww - mw) / 2) , by0 - 48 - (my1 - my0) * S
sx1, sy1 = max(ww, (ww + mw) / 2), by1
write("pergent-stacked", move(word_s, PAD - sx0, PAD - sy0), R,
      (0, 0, sx1 - sx0 + 2 * PAD, sy1 - sy0 + 2 * PAD),
      extra=[(move(mark_s, PAD - sx0, PAD - sy0), R * S)])

print("written to", OUT)
