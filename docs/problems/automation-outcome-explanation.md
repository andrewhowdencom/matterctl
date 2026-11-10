# Explaining an unexpected automation outcome

Parent: [Keeping the house manageable](management-overhead.md)

When Den Light unexpectedly turns off, the owner wants to know what the
presence sensor reported, which rule reacted, and what happened to the light.
Live automation outcomes currently appear only in the service log. Finding
and interpreting those entries adds diagnostic work at the time the household
already needs a correction.

## Measure

From noticing an unexpected change, measure the time and steps needed to
identify the relevant sensor report, rule decision, command result, and any
observation gap. Record whether service logs were needed and whether the cause
remained unknown. The current time baseline is not measured. The proposed
target is an answer within one minute without service logs for the Den Light
case; an explicit “evidence missing” is a valid answer when records are
incomplete.

## Candidate feature

[Automation activity and history](https://github.com/andrewhowdencom/matterctl/pull/29)
should provide a whole-house timeline and automation status through both CLI
and browser. Test the one-minute target with a real Den Light incident and
include the feature's storage and maintenance cost when assessing the parent
problem.
