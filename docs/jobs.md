# Jobs

Long-running operations run in the background as **jobs**, so you can close the browser while they continue:

- Talos rolling upgrades
- Config patch rollouts
- Reboots

## Following a job

Starting an operation takes you to its job page. The log updates every few seconds and shows each step: which node is being processed, what is being waited for (`… node offline`, `… NotReady`) and the result per node (✓ / ✗).

**Jobs** in the sidebar lists recent jobs with status (**running**, **succeeded**, **failed**, **cancelled**), start time and duration.

## Cancelling

**Cancel** stops the job after the current step. A node that has already started rebooting into a new version or config finishes doing so; the job just doesn't continue to the next node.

## History

The last 50 jobs and their logs are kept in the data directory and survive restarts. A job that was running when the container restarted is marked **failed** with "interrupted by restart" – check the affected node before starting again.

Only one upgrade or config rollout can run at a time; starting another returns an error until the first one finishes. Reboots can run alongside.
