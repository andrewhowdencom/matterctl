# Keeping the house manageable

Parent: [Household problem](../../PROBLEM.md)

The effort of understanding and adapting the house can erase the benefit of a
convenient response.

## Configuring behavior

Home Assistant could provide the desired flexibility, but the owner found its
broad model conceptually too complex. This project's rule files also require
editing, checking, and restarting the service to activate a change. The effort
saved by a simpler rule-management workflow has not yet been tested.

Measure the steps and elapsed time from deciding on a rule change to observing
the new behavior; no baseline has been recorded.

## Finding devices

The owner would like to see lights room by room. This suggests that a
device-first list may become hard to navigate as the home grows. The practical
cost of locating lights in the current view has not yet been established.

Measure time and navigation steps to find and control a chosen room's light;
no baseline has been recorded.

## More specific problem

- [Explaining an unexpected automation outcome](automation-outcome-explanation.md):
  the owner needs to discover why Den Light turned off without searching
  service logs.

Diagnosing [an automation affected by a device interruption](switch-control-recovery.md)
also adds upkeep: an available device can still have an inactive rule, so the
owner must locate the failed part before restoring control.

## Measures

Measure the time and steps needed to find the right device, create or change a
behavior, and explain an unexpected result. Include monthly maintenance time
and extra equipment when comparing approaches. The owner has reported Home
Assistant's conceptual complexity and the current rule-edit/restart steps;
none of these task times has a measured baseline yet.
