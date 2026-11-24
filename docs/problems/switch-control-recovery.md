# A device interruption prevents a needed response

Parent: [Getting a response when needed](access-and-timing.md)

When a device becomes unreachable or the home control system restarts, an
automation that worked before may stop producing a needed physical response.
It may remain ineffective after the device returns. A familiar switch press or
sensor change produces no response, so someone uses another control or must
diagnose and restore the setup. One unavailable device can also interrupt
otherwise working controls.

On the observed November 21 reboot, two BILRESA switches remained unavailable.
Den Switch was reported available, but its shared automation file did not
activate because the Bedroom Switch label was absent. That file also controls
other rooms. Matter Server normally reports a returning device through a node
update; the current automation runner does not recheck bindings on that update.
We have not observed the Bedroom Switch return, so the recovery gap is inferred
from the current code rather than measured in the household.

## Measure

After each restart or device interruption, make a new request through each
affected control or observe a new sensor change. Record whether it produces the
intended physical response. Count missed responses, otherwise working controls
affected by another device, alternate control actions, and the time from device
return to normal behavior. The baseline is one observed reboot with unavailable
switches and an inactive shared automation file; no failure rate or recovery
time has been measured.

## Related problem

[Keeping the house manageable](management-overhead.md) includes the effort of
finding which device, connection, or rule prevented the response and restoring
it. Record diagnostic and repair time separately from the household interruption.
