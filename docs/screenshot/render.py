"""Render a tmux `capture-pane -e` dump into a framed HTML terminal window.

Used by make.sh: python3 render.py capture.ansi page.html
"""
import html
import re
import sys

PALETTE = [
    "#414868", "#f7768e", "#9ece6a", "#e0af68", "#3d59a1", "#bb9af7", "#7dcfff", "#a9b1d6",
    "#565f89", "#ff899d", "#9fe044", "#faba4a", "#7aa2f7", "#c7a9ff", "#a4daff", "#c0caf5",
]
FG, BG = "#c0caf5", "#1a1b26"


def color256(n):
    if n < 16:
        return PALETTE[n]
    if n < 232:
        n -= 16
        steps = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (steps[n // 36], steps[(n // 6) % 6], steps[n % 6])
    v = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (v, v, v)


def render(text):
    state = {"fg": None, "bg": None, "bold": False, "faint": False, "rev": False, "under": False}
    out_lines = []
    for line in text.rstrip("\n").split("\n"):
        parts = re.split(r"\x1b\[([0-9;]*)m", line)
        buf = []
        for i, part in enumerate(parts):
            if i % 2 == 1:
                apply(state, part)
                continue
            if not part:
                continue
            fg, bg = state["fg"] or FG, state["bg"]
            if state["rev"]:
                fg, bg = (bg or BG), (state["fg"] or FG)
            style = [f"color:{fg}"]
            if bg:
                style.append(f"background:{bg}")
            if state["bold"]:
                style.append("font-weight:700")
            if state["faint"]:
                style.append("opacity:.6")
            if state["under"]:
                style.append("text-decoration:underline;text-underline-offset:3px")
            buf.append(f'<span style="{";".join(style)}">{html.escape(part)}</span>')
        out_lines.append("".join(buf))
    return "\n".join(out_lines)


def apply(state, params):
    codes = [int(c) if c else 0 for c in params.split(";")] if params else [0]
    i = 0
    while i < len(codes):
        c = codes[i]
        if c == 0:
            state.update(fg=None, bg=None, bold=False, faint=False, rev=False, under=False)
        elif c == 1:
            state["bold"] = True
        elif c == 2:
            state["faint"] = True
        elif c == 22:
            state["bold"] = state["faint"] = False
        elif c == 4:
            state["under"] = True
        elif c == 24:
            state["under"] = False
        elif c == 7:
            state["rev"] = True
        elif c == 27:
            state["rev"] = False
        elif 30 <= c <= 37:
            state["fg"] = PALETTE[c - 30]
        elif 90 <= c <= 97:
            state["fg"] = PALETTE[c - 90 + 8]
        elif c == 39:
            state["fg"] = None
        elif 40 <= c <= 47:
            state["bg"] = PALETTE[c - 40]
        elif 100 <= c <= 107:
            state["bg"] = PALETTE[c - 100 + 8]
        elif c == 49:
            state["bg"] = None
        elif c in (38, 48) and i + 1 < len(codes):
            key = "fg" if c == 38 else "bg"
            if codes[i + 1] == 5 and i + 2 < len(codes):
                state[key] = color256(codes[i + 2])
                i += 2
            elif codes[i + 1] == 2 and i + 4 < len(codes):
                state[key] = "#%02x%02x%02x" % tuple(codes[i + 2:i + 5])
                i += 4
        i += 1


PAGE = """<!doctype html><html><head><meta charset="utf-8"><style>
html,body{{margin:0;background:transparent}}
.win{{display:inline-block;margin:28px;border-radius:12px;background:{bg};
  box-shadow:0 18px 50px rgba(0,0,0,.45),0 0 0 1px rgba(255,255,255,.06)}}
.bar{{height:34px;display:flex;align-items:center;padding:0 14px;gap:8px;
  border-bottom:1px solid rgba(255,255,255,.05)}}
.dot{{width:12px;height:12px;border-radius:50%}}
.title{{flex:1;text-align:center;margin-right:52px;color:#787c99;
  font:13px "JetBrainsMono Nerd Font Mono",monospace}}
pre{{margin:0;padding:12px 16px 16px;color:{fg};
  font:14px/1.2 "JetBrainsMono Nerd Font Mono",monospace;letter-spacing:0;
  font-variant-ligatures:none;font-feature-settings:"calt" 0,"liga" 0}}
</style></head><body><div class="win" id="win">
<div class="bar"><span class="dot" style="background:#ff5f57"></span>
<span class="dot" style="background:#febc2e"></span><span class="dot" style="background:#28c840"></span>
<span class="title">lazycompose</span></div><pre>{body}</pre></div></body></html>"""

if __name__ == "__main__":
    src, dst = sys.argv[1], sys.argv[2]
    with open(src, encoding="utf-8") as f:
        body = render(f.read())
    with open(dst, "w", encoding="utf-8") as f:
        f.write(PAGE.format(bg=BG, fg=FG, body=body))
