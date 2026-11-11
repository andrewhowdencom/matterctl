# Getting a response that fits

Parent: [Household problem](../../PROBLEM.md)

The house should respond at the right time and in the right way for the people
using it. That requires useful cues and a way to express the intended behavior.

## Missing cues or behaviors

The owner wants automations more sophisticated than Google Home supported and
access to a wider range of sensor information. The specific household behaviors
and sensor inputs still need to be described before this problem can be tested
against an implementation.

Settling the baby involves dimming lights as a sleep cue and starting music,
which Google Home currently handles. This is one household outcome spanning
different devices; it does not require one controller to provide every part.

For a specific desired behavior, count situations where the needed cue is
available and the intended outcome occurs. No such behavior has been measured
against this project yet.

## More specific problem

- [An automatic light change at the wrong time](unwanted-light-changes.md):
  Den Light sometimes turns off while the owner is still in the room.

## Measures

For each described situation, count intended outcomes reached, needs missed,
and unwanted responses. Record the time and actions needed to restore a useful
state after an unwanted response. Assess the household result, not just
whether a rule executed. No numeric baseline is recorded yet; the Den Light
surprise is currently described as occasional.
