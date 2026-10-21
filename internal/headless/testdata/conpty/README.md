# ConPTY captures

Raw pty output as `runner.Client` receives it, before any filter. Captured on Windows 11 Pro 10.0.26300 (the conhost
of Windows), with the local runner of `runtree-headless` f9b2dd8 and `--log-db`: the `output` table, start 1, the chunks
concatenated in the order of their ids. `chunks.json` has the sizes of the chunks as they arrived, for tests of
writes that are split.

**Pty size: 120 columns x 40 rows.** That is `winSize` of `runtree-headless` (internal/headless/headless.go:36), sent
as `InitialPtySize`; 80x24 is only the fallback of the runner when the size is 0. The Write-Progress pane shows the
width: 600 characters from `ESC[3;1H` are the 5 rows 3-7 by autowrap.

In the expected lines below, `{...}` is the color of the text that follows it, `{}` is no color.

## 1a-powershell-steps.bin

    powershell -NoProfile -Command "1..3 | ForEach-Object { Write-Host \"step $_\" -ForegroundColor Green; Start-Sleep -Milliseconds 300 }"

Expected:

    {38;5;10}step 1
    {38;5;10}step 2
    {38;5;10}step 3

The color of "step 1" is set before the cursor move: `ESC[m ESC[38;5;10m ESC[H step 1`. For lines 2 and 3 it comes
after the CRLF.

## 1b-powershell-colors.bin

    powershell -NoProfile -Command "Write-Host red -ForegroundColor Red; Write-Host green -ForegroundColor Green -NoNewline; Write-Host ' then plain'; Write-Host blue-on-yellow -ForegroundColor Blue -BackgroundColor Yellow; Write-Output plain"

Expected:

    {38;5;9}red
    {38;5;10}green{} then plain
    {38;5;12;48;5;11}blue-on-yellow
    plain

The color of "red" is set before `ESC[H`. The colors of "green" and of "blue-on-yellow" are set at the end of the line
before, before the CRLF: `red ... ESC[m ESC[38;5;10m \r\n green ESC[m then plain ESC[38;5;12m ESC[48;5;11m \r\n
blue-on-yellow ESC[m`. ConPTY also writes the space before "then plain" in green, with the reset after it.

## 1c-cmd-ansi.bin

    cmd /c "echo ESC[31mred ESC[0m plain & echo ESC[42mgreen bg ESC[0m & echo ESC[1;34mbold blue ESC[0m end"

(the YAML had `\e`). Expected (cmd's echo also writes the space before `&`, ConPTY drops it at the line end):

    {31}red{} plain
    {42}green bg{}
    {34;1}bold blue{} end

`ESC[31m` comes before `ESC[H`, and `ESC[34m ESC[1m` before the CRLF of the line above. In cmd, the window title
(OSC 0) comes after the first line.

## 2-scroll-progress.bin

    powershell -NoProfile -Command "1..40 | % { \"line $_\" }; for ($i = 0; $i -le 100; $i += 50) { Write-Progress -Activity Working -PercentComplete $i; Start-Sleep -Milliseconds 300 }; 'after 1'; 'after 2'"

Expected (the progress pane is temporary: no lines, or a few):

    line 1
    ...
    line 40
    after 1
    after 2

The CRLF after "line 40" on row 40 scrolls the screen: the cursor stays on row 40 (empty), and rows 1-39 are lines
2-40. Then:
- 3 updates of the pane: `ESC[38;5;11m ESC[48;5;6m ESC[3;1H` and 600 characters (rows 3-7 by autowrap), then
  `ESC[40;1H`, back to the current line;
- after the pane, rows 3-7 are repainted twice with what is under it: `ESC[3;1H line 4<114 spaces>line 5 ...
  line 8<114 spaces> ESC[40;1H`, with no colors;
- `after 1` on row 40, ended by `\r ESC[?25h ESC[?25l \n`: CR and LF with sequences between them. The LF scrolls;
- rows 3-7 are repainted again after each scroll (`line 5 ... line 9`, then `line 6 ... line 10`), then `after 2`
  in the same way.

## 3-progress.bin

    powershell -NoProfile -Command "for ($i = 0; $i -le 100; $i += 20) { Write-Progress -Activity Working -Status \"$i percent\" -PercentComplete $i; Start-Sleep -Milliseconds 300 }; Write-Output done"

Expected:

    done

The pane's text ("Working", "N percent", "[ooo...]") may be kept as a few lines, but "done" must be in the log.

No scrolling. The screen is empty when the pane is drawn on rows 3-7 (`ESC[3;1H`, 600 characters), and then
`ESC[H`. Each update redraws the pane in the same way. The pane is erased with `ESC[3;1H` and 600 spaces, then
`ESC[H`. **"done" is written on row 1, above the rows the pane used**, and **its line is ended only by the final
`ESC[2;1H`**, not by a CRLF. Before that, the pane rows are erased again with `ESC[3;1H` and 600 spaces. A model that
logs the pane rows as lines and drops text above the current log line as a repaint drops "done". Row 1 was empty until
then.

## 4-cmd-timeout.bin

    cmd /c "echo before & timeout /t 3 & echo after"

Expected:

    before

    Waiting for 3 seconds, press a key to continue ...
    after

The last screen has "Waiting for 0 seconds, ..." in its line. Either one is fine.

timeout.exe starts with a newline, which is the empty row 2. It writes "Waiting for 3 seconds, ..." on row 3. Then
`ESC[3;14H` moves within the same row, to column 14. Then `BS 2 BS 1 BS 0`: each backspace goes to column 13 (the
"3"), and the digit overwrites it. CRLF and "after" follow.
