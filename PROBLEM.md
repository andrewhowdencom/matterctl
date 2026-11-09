# Problem

I want to control the lights in my home promptly and locally, and build
automations that can use the sensors and behaviors I care about. My existing
options leave a gap: Google Home feels too slow for light control and does not
offer the automation flexibility or sensor support I want. Home Assistant could
address some of those needs, but its broad integration model feels too complex
for a home where I want to work directly with Matter devices.

This is the problem matterctl is exploring: how to get responsive local control
and useful sensor-driven automation without taking on more concepts and
operational complexity than the job requires.

## What we know

- The project began with the owner's experience of slow light control through
  Google Home and limits in its automations and sensor support.
- After using local control, the owner observed the physical light change in
  roughly 100 ms, compared with seconds previously through Google Home. This is
  an informal estimate, not a timed comparison.
- Home Assistant was considered as a possible solution, but its breadth and
  conceptual complexity were a poor fit for the owner's desired scope.
- Matter-only control is the proposed approach. Whether it is sufficient for
  the devices and automations the owner ultimately wants remains to be tested.

## How to assess progress

These are dimensions to observe, not targets established in advance:

- Time from requesting a light action to observing the device's reported state,
  alongside whether the physical light responds.
- Whether the desired automations can be expressed and run reliably using the
  available Matter events, attributes, and commands.
- Whether the sensors the owner wants to use expose the needed data to those
  automations.
- The effort required to add a device, understand its capabilities, create an
  automation, and keep the system running.

The owner's observations establish the starting problem and suggest a substantial
improvement in response time. A repeatable latency comparison, the full set of
desired automations and sensors, and an acceptable complexity level have not yet
been measured or defined.
