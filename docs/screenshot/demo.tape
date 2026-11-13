# The demo GIF in the README, recorded by "make.sh gif" with vhs
# (https://github.com/charmbracelet/vhs) against the demo stacks.

Output demo.gif

Set Shell bash
Set FontFamily "JetBrains Mono"
Set FontSize 16
Set Width 1300
Set Height 780
Set Padding 18
Set WindowBar Colorful
Set BorderRadius 10
Set Framerate 24
Set TypingSpeed 110ms
Set Theme { "name": "lazycompose", "background": "#1a1b26", "foreground": "#c0caf5", "selection": "#33467c", "cursor": "#c0caf5", "black": "#414868", "red": "#f7768e", "green": "#9ece6a", "yellow": "#e0af68", "blue": "#3d59a1", "magenta": "#bb9af7", "cyan": "#7dcfff", "white": "#a9b1d6", "brightBlack": "#565f89", "brightRed": "#ff899d", "brightGreen": "#9fe044", "brightYellow": "#faba4a", "brightBlue": "#7aa2f7", "brightMagenta": "#c7a9ff", "brightCyan": "#a4daff", "brightWhite": "#c0caf5" }

Env COLORTERM truecolor

Hide
Type "clear && lazycompose -root /srv/stacks -config /dev/null"
Enter
Sleep 5s
Show

# the start screen, then the logs follow the cursor
Sleep 2.5s
Down
Sleep 1.5s
Down
Sleep 1.5s
Down
Sleep 1.5s
Up@400ms 2
Sleep 1s

# take jellyfin down and up again
Type "d"
Sleep 1.2s
Type "y"
Sleep 3s
Type "u"
Sleep 3s

# edit its compose file: move the port, save, and apply the change
Type "e"
Sleep 1.8s
Type "/8096:"
Sleep 0.4s
Enter
Sleep 0.6s
Type "3l"
Sleep 0.5s
Type "r7"
Sleep 1.5s
Type ":wq"
Sleep 0.6s
Enter
Sleep 1.8s
Enter
Sleep 3.5s

# the services pane, then the compose file of a service, zoomed
Tab
Sleep 1.2s
Type "v"
Sleep 2s
Tab
Sleep 0.6s
Type "z"
Sleep 2.5s
Type "z"
Sleep 0.6s
Type "v"
Sleep 1s
Tab
Sleep 0.8s

# run any action by name, and search the keys
Type ":"
Sleep 0.6s
Type "pull"
Sleep 2s
Escape
Sleep 0.6s
Type "?"
Sleep 0.6s
Type "update"
Sleep 2.5s
Escape
Sleep 0.6s

# filter the list
Type "/"
Sleep 0.4s
Type "arr"
Sleep 2s
Escape
Sleep 1.5s
