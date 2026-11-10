# An automatic light change at the wrong time

Parent: [Getting a response that fits](contextual-response.md)

The owner is occasionally surprised when the Den presence automation turns
Den Light off while they are still in the room. The light no longer fits their
immediate need, and restoring it creates an interruption. We do not yet know
whether the sensor reported unoccupied or the rule acted on an unexpected
input; this observation does not establish that either is faulty.

## Measure

Count switch-offs while someone still needs the light, the manual actions and
time needed to restore it, and the period the room remains in the unwanted
state. The current baseline is “occasionally”; no frequency or recovery time
has been measured. Record incidents over a representative period before and
after a behavioral fix.

## Candidate feature

[Automation activity and history](https://github.com/andrewhowdencom/matterctl/pull/29)
should reveal the observed input and rule action and let the owner pause the
responsible file. This helps diagnose and contain the response. It does not
correct a false sensor reading or change the rule's intended behavior. A later
fix should be judged by a lower rate of unwanted switch-offs and recovery
effort, not merely by the presence of a history entry.
