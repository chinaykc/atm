# Release Risk Planner Fan-out

This queue lets one planning pass return review shards, then expands those shards into parallel reviewer branches.

## /def plan_shards

Inspect the current release diff and split the risk review into independent shards.

Each plan must include the reviewer focus, owner, key question, and write directory under ./result.

/return
```
plans:[]string:Review plans
```

## //parallel shard review

/pool reviewer 4 20

/for plan in(/call plan_shards)
/go reviewer
{{plan}}

/wait reviewer

## /merge decision

Read the files under ./result and produce the release decision.

Include:

- blocking findings first
- non-blocking follow-up work
- exact owner for each next step
- whether the release can continue today
