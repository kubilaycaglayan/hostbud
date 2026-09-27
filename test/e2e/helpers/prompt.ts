import { shq, type Target } from './target.ts'

// A deterministic multiline prompt for the throwaway target, drawn like the
// input box of Codex or Claude Code: a bordered box, "> " before the first
// line, left/right crossing line ends, typed characters inserted at the caret.
// After every key it writes "line col" (and "vertical" once an up/down arrow
// arrived) to its state file. Ctrl-C quits. perl-base is in the target image.
const SCRIPT = String.raw`
use strict; use warnings;
my $state = shift;
my @lines = ('first line of the prompt', 'second', 'a third line here', 'end');
my ($l, $c, $vertical) = (3, 3, 0);
system('stty', 'raw', '-echo');
$| = 1;
sub draw {
  print "\e[H\e[2J", "prompt-like input\r\n", '+', '-' x 40, "+\r\n";
  printf "| %s %-37s|\r\n", ($_ == 0 ? '>' : ' '), $lines[$_] for 0 .. $#lines;
  print '+', '-' x 40, "+\r\n";
  printf "\e[%d;%dH", 3 + $l, 5 + $c;
  open my $f, '>', "$state.tmp" or die; print $f "$l $c", ($vertical ? ' vertical' : ''); close $f;
  rename "$state.tmp", $state;
}
draw();
while (sysread(STDIN, my $b, 1)) {
  last if $b eq "\x03";
  if ($b eq "\e") {
    sysread(STDIN, my $s, 2);
    my $k = substr($s, 1, 1);
    if ($k eq 'C') { if ($c < length $lines[$l]) { $c++ } elsif ($l < $#lines) { $l++; $c = 0 } }
    elsif ($k eq 'D') { if ($c > 0) { $c-- } elsif ($l > 0) { $l--; $c = length $lines[$l] } }
    elsif ($k eq 'A' || $k eq 'B') { $vertical = 1 }
  } elsif ($b =~ /[ -~]/) { substr($lines[$l], $c, 0) = $b; $c++ }
  draw();
}
system('stty', 'sane');
print "\r\n";
`

export const PROMPT_TITLE = 'prompt-like input'
export const PROMPT_LINES = ['first line of the prompt', 'second', 'a third line here', 'end']

/** Installs the prompt on the target; returns the command that runs it. */
export async function installPrompt(target: Target, state: string): Promise<string> {
  await target.run(`cat > /tmp/hostbud-prompt.pl <<'HOSTBUD_EOF'\n${SCRIPT}\nHOSTBUD_EOF`)
  await target.run(`rm -f ${shq(state)}`)
  return `perl /tmp/hostbud-prompt.pl ${shq(state)}`
}

/** The prompt's caret as "line col" (plus " vertical" if up/down arrived). */
export async function promptCaret(target: Target, state: string): Promise<string> {
  return (await target.exec(`cat ${shq(state)} 2>/dev/null`)).stdout.trim()
}
