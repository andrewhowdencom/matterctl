# Household problem

In recurring situations, the house does not always meet the household's needs
with little effort. Getting the right outcome can require interruption,
movement, waiting, or remembering an action. Automation can help, but wrong
responses or too much upkeep can erase the gain.

## Subproblems

- [Getting a response when needed](docs/problems/access-and-timing.md): a
  needed change is awkward to request, slow to take effect, or forgotten.
- [Getting a response that fits](docs/problems/contextual-response.md): the
  house lacks the cues or behavior needed to respond appropriately.
- [Keeping the house manageable](docs/problems/management-overhead.md): finding
  devices, configuring behavior, and understanding outcomes take too much work.

Each linked problem can contain more specific problems. The links express the
hierarchy; the examples and evidence live with the problem they support.
Link a proposed feature from the most specific problem it should address,
stating whether it should resolve, reduce, or only help diagnose that problem.
Feature design and delivery details belong in the relevant pull request;
implemented behavior belongs in user documentation.

## Measuring the root problem

For each recurring household situation, record the number of times the house
reaches the useful state, interruptions or manual recovery actions per
occurrence, time from a request to the physical result where applicable, and
unwanted responses per week. Compare those gains with time spent configuring
and maintaining the approach and any additional equipment. Record a baseline
before claiming improvement; the current examples are qualitative accounts
from the owner, not a measured household baseline. Other household members'
needs require their own evidence.

Every linked problem should state an observable situation and consequence, a
measure that could change, and the current baseline or the fact that it is not
yet known. A feature succeeds only if the measure improves without adding
more interruption or upkeep than it removes.

A lamp, different switch wiring, Google Home, Home Assistant, and a Matter
controller are all possible approaches to parts of this problem. Local control
and a Matter-only model are the current project's choices, not the problem
itself.
