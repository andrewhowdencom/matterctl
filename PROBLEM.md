# Problem

In everyday situations, getting the house to do what I need takes more attention
or movement than I want. I may be holding the baby and need to set the lights,
or be in bed and want to turn one off. I want the house to respond appropriately
when it can, and to be easy to direct and configure when it cannot. The effort,
equipment, and complexity involved should be justified by the usefulness I get
back.

## Subproblems

1. **Getting the right response in the moment takes too much effort.** A control
   may be out of reach when my hands are occupied or I am in bed. A response
   that takes seconds interrupts the moment even when I can issue a command.
2. **The house does not always respond to useful context.** I want to define
   behaviors using the lights and sensors that matter to me. Google Home's
   automation and sensor support did not let me express everything I wanted.
3. **Configuring and understanding the house can take too much effort.** Home
   Assistant offered a possible way to build the behaviors, but its breadth
   felt conceptually too complex for what I wanted. As this project grows, the
   effort of finding devices, changing rules, and understanding outcomes also
   needs attention. Seeing lights by room is a candidate need within this
   subproblem; its practical impact has not yet been established.

These subproblems describe different sources of effort beneath the same
household problem. They can be tested separately without treating any one
control system or interface as inevitable.

## Evidence so far

- The baby and bed situations are the owner's examples of when convenient
  response matters.
- Google Home light changes previously felt seconds long. After using local
  control, the owner observed the physical light change in roughly 100 ms. This
  is an informal estimate, not a timed comparison.
- The owner found Home Assistant conceptually heavy and Google Home too limited
  for the desired automations and sensors. The exact behaviors and sensor set
  still need to be described.

## How to assess progress

For representative household situations, observe how often a person must stop,
reach, get up, or repeat an action; how long the house takes to respond; and
whether the response is appropriate. Compare those gains with the effort of
setting up, understanding, and maintaining the solution and with any extra
equipment it requires. The room-view and automation-operability proposals need
their own task-level evidence of reduced effort.

A lamp, different switch wiring, Google Home, Home Assistant, and a Matter
controller are all possible approaches to parts of this problem. Local control
and a Matter-only model are the current project's choices, not the problem
itself.
