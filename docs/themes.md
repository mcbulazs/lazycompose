# Themes

[README](../README.md) · [Usage](usage.md) · [Configuration](configuration.md) · [Keys](keys.md) · **Themes** · [Shell integration](shell.md) · [Development](development.md)

Built-in themes: `terminal`, `onedark`, `tokyonight`, `catppuccin`, `gruvbox`
and `nord`. Pick one with `theme` in the config, or try one with
`lazycompose -theme nord`.

The default `terminal` theme uses your terminal's own colours, like lazygit
does: a green border on the active pane and a blue selected line. The others
use fixed colours. None of them change the terminal background, so transparent
terminals keep working.

## Your own themes

Add a theme under `themes` and select it with `theme`. It starts from the
built-in theme in `base` (`terminal` if left out) and changes only the colours
you list:

```yaml
theme: sunset
themes:
  sunset:
    base: onedark
    focus: "#ff9e64"
    select_bg: "#3b3052"
```

You can define several and switch between them with `theme`.

## Changing single colours

`colors` changes colours of whichever theme is active, on top of the theme:

```yaml
theme: nord
colors:
  focus: "#ebcb8b"
```

## Colours

Colours are hex values (`"#ff9e64"`, quoted) or terminal colour numbers from
0 to 255.

| Key | Used for |
| --- | --- |
| `border` | Borders of panes that are not focused |
| `focus` | Border and title of the focused pane, keys in the footer |
| `text` | Normal text |
| `dim` | Secondary text and group names |
| `green` | Running services |
| `yellow` | Partly running environments, questions and search matches |
| `red` | Errors and environments with problems |
| `select_bg` | Background of the selected row |
| `select_fg` | Text of the selected row |

A typo in a colour name or value is reported when lazycompose starts.
