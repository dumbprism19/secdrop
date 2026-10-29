#!/usr/bin/env python3
"""Turn secdrop screen renders (ANSI, 24-bit colour) into SVG terminal windows.

    SECDROP_SHOTS=/tmp/shots go test ./internal/ui -run Screenshots
    python3 scripts/ansi2svg.py /tmp/shots assets/screens
"""
import html
import os
import re
import sys

CW, LH, FS = 8.4, 18, 14          # cell width, line height, font size
PAD, BAR = 18, 34                   # inner padding, title bar height
BG, FG = "#1C1A17", "#E6DFD3"       # terminal background and default text
FONT = "SFMono-Regular, Menlo, Consolas, 'DejaVu Sans Mono', 'Liberation Mono', monospace"

SGR = re.compile(r"\x1b\[([0-9;]*)m")


def parse(line):
    """Yield (text, fg, bg, bold, underline) runs for one line."""
    fg = bg = None
    bold = under = False
    pos = 0
    for m in SGR.finditer(line):
        if m.start() > pos:
            yield line[pos:m.start()], fg, bg, bold, under
        codes = [int(c) if c else 0 for c in m.group(1).split(";")]
        i = 0
        while i < len(codes):
            c = codes[i]
            if c == 0:
                fg = bg = None
                bold = under = False
            elif c == 1:
                bold = True
            elif c == 22:
                bold = False
            elif c == 4:
                under = True
            elif c == 24:
                under = False
            elif c in (38, 48) and i + 4 < len(codes) and codes[i + 1] == 2:
                col = "#%02x%02x%02x" % tuple(codes[i + 2:i + 5])
                if c == 38:
                    fg = col
                else:
                    bg = col
                i += 4
            elif c == 39:
                fg = None
            elif c == 49:
                bg = None
            i += 1
        pos = m.end()
    if pos < len(line):
        yield line[pos:], fg, bg, bold, under


def convert(src, dst):
    lines = open(src, encoding="utf-8").read().split("\n")
    cols = max(len(SGR.sub("", l)) for l in lines)
    w = int(PAD * 2 + cols * CW)
    h = int(BAR + PAD + len(lines) * LH + PAD)
    out = [
        f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}">',
        f'<rect width="{w}" height="{h}" rx="12" fill="{BG}"/>',
        f'<rect width="{w}" height="{BAR}" rx="12" fill="#262320"/>',
        f'<rect y="{BAR - 12}" width="{w}" height="12" fill="#262320"/>',
    ]
    for i, col in enumerate(("#FF5F57", "#FEBC2E", "#28C840")):
        out.append(f'<circle cx="{20 + i * 20}" cy="{BAR / 2}" r="6" fill="{col}"/>')
    out.append(f'<text x="{w / 2}" y="{BAR / 2 + 5}" fill="#8E8473" font-family="{FONT}" font-size="13" text-anchor="middle">secdrop</text>')
    out.append(f'<g font-family="{FONT}" font-size="{FS}" xml:space="preserve">')
    for row, line in enumerate(lines):
        y = BAR + PAD + row * LH
        col = 0
        for text, fg, bg, bold, under in parse(line):
            n = len(text)
            x = PAD + col * CW
            if bg:
                out.append(f'<rect x="{x:.1f}" y="{y}" width="{n * CW:.1f}" height="{LH}" fill="{bg}"/>')
            # Place each word at its own column, so fonts with slightly
            # different widths can't drift or squash the layout.
            for word in re.finditer(r"\S+", text):
                wx = PAD + (col + word.start()) * CW
                attrs = f'x="{wx:.1f}" y="{y + LH - 5}" fill="{fg or FG}"'
                if bold:
                    attrs += ' font-weight="bold"'
                if under:
                    attrs += ' text-decoration="underline"'
                out.append(f"<text {attrs}>{html.escape(word.group())}</text>")
            col += n
    out.append("</g></svg>")
    open(dst, "w", encoding="utf-8").write("\n".join(out) + "\n")


def main():
    src, dst = sys.argv[1], sys.argv[2]
    os.makedirs(dst, exist_ok=True)
    for name in sorted(os.listdir(src)):
        if name.endswith(".ansi"):
            convert(os.path.join(src, name), os.path.join(dst, name[:-5] + ".svg"))
            print("wrote", os.path.join(dst, name[:-5] + ".svg"))


if __name__ == "__main__":
    main()
