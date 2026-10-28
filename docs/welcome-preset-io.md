# Welcome preset hint I/O failure inventory

Written before changing the hint implementation:

- The welcome View calls presetSuggestion, which stats home-directory configs
  and opens tmux.conf on the UI goroutine. A slow mount stalls input/rendering.
- tmuxPrefix opens the path before checking that its descriptor is regular;
  opening a FIFO with no writer blocks before the check can reject it.
- A cached suggestion makes this a first-use stall; later frames appear normal.
- Offloading the work must preserve symlinks to ordinary dotfiles and the
  configured tmux leader; a pipe/device must not leave a stuck worker.
- Learning a suggestion is advisory and must never apply a preset by itself.
- A late probe result must not switch a client back after a user chose keys.

The downstream real-PTY regression supplies a private HOME containing a FIFO
tmux.conf, switches to the outer welcome screen and asks for its help overlay.
It starts no golder agent. This deterministically models blocked home-config
I/O, not the exact cause on the reported Linux server; server evidence remains
necessary to establish that correspondence.
