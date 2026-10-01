# Wallet

This is some test assignment. The full spec can be found in [docs](docs/spec.md), as well as the decisions I made.

## Performance tests

While the chosen write strategy may seem overengineered, there is a reason.

The old simple design was making a db write for every request, awaiting for WAL to be flushed. Which takes about 1.4ms on my WSL2 (I asked Claude to measure).

That means, there is a hard limit on my system to about ~700 rps. But I wanted 1000.

So, together with Claude, we agreed on the following strategy:

- Each wallet gets its own worker.
- Worker writes all updates for this wallet in batches, aka in one transaction.
- While worker is busy, new incoming requests for this wallet are collected in a queue.

Of course, we clean up workers and queues when they are no longer needed, and so on.

Load tests with k6 before and after.

<details>
 <summary>Old design (awaiting WAL flush for every incoming write request)</summary>

```

         /\      Grafana   /‾‾/
    /\  /  \     |\  __   /  /
   /  \/    \    | |/ /  /   ‾‾\
  /          \   |   (  |  (‾)  |
 / __________ \  |_|\_\  \_____/


     execution: local
        script: /scripts/wallet.js
        output: -

     scenarios: (100.00%) 1 scenario, 500 max VUs, 1m30s max duration (incl. graceful stop):
              * one_wallet: 333.33 iterations/s for 1m0s (maxVUs: 100-500, gracefulStop: 30s)

WARN[0008] Insufficient VUs, reached 500 active VUs and cannot initialize more  executor=constant-arrival-rate scenario=one_wallet
INFO[0061] start balance 1000000, final balance 998387   source=console


  █ THRESHOLDS

    checks
    ✗ 'rate==1' rate=99.05%

    dropped_iterations
    ✗ 'count==0' count=5590

    http_req_failed
    ✗ 'rate==0' rate=1.41%


  █ TOTAL RESULTS

    checks_total.......: 129697 2118.118316/s
    checks_succeeded...: 99.05% 128472 out of 129697
    checks_failed......: 0.94%  1225 out of 129697

    ✓ not 5xx
    ✗ not 429
      ↳  98% — ✓ 42620 / ✗ 612
    ✗ status 200
      ↳  98% — ✓ 42620 / ✗ 612
    ✗ final balance equals start balance
      ↳  0% — ✓ 0 / ✗ 1

    HTTP
    http_req_duration..............: avg=656.73ms med=623.98ms p(95)=928.97ms p(99)=2s    max=6.53s
      { expected_response:true }...: avg=637.43ms med=622.58ms p(95)=864.77ms p(99)=1.99s max=6.53s
    http_req_failed................: 1.41%  612 out of 43232
    http_reqs......................: 43232  706.033995/s

    EXECUTION
    dropped_iterations.............: 5590   91.291868/s
    iteration_duration.............: avg=1.97s    med=1.93s    p(95)=2.87s    p(99)=5.86s max=7.49s
    iterations.....................: 14410  235.333777/s
    vus............................: 266    min=113          max=500
    vus_max........................: 500    min=115          max=500

    NETWORK
    data_received..................: 7.7 MB 126 kB/s
    data_sent......................: 8.0 MB 130 kB/s




running (1m01.2s), 000/500 VUs, 14410 complete and 0 interrupted iterations
one_wallet ✓ [======================================] 000/500 VUs  1m0s  333.33 iters/s
ERRO[0061] thresholds on metrics 'checks, dropped_iterations, http_req_failed' have been crossed

```

</details>

<details>
 <summary>Current design (write requests for one wallet are queued up and executed in batches)</summary>

```

         /\      Grafana   /‾‾/
    /\  /  \     |\  __   /  /
   /  \/    \    | |/ /  /   ‾‾\
  /          \   |   (  |  (‾)  |
 / __________ \  |_|\_\  \_____/


     execution: local
        script: /scripts/wallet.js
        output: -

     scenarios: (100.00%) 1 scenario, 500 max VUs, 1m30s max duration (incl. graceful stop):
              * one_wallet: 333.33 iterations/s for 1m0s (maxVUs: 100-500, gracefulStop: 30s)

INFO[0060] start balance 1000000, final balance 1000000  source=console


  █ THRESHOLDS

    checks
    ✓ 'rate==1' rate=100.00%

    dropped_iterations
    ✓ 'count==0' count=0

    http_req_failed
    ✓ 'rate==0' rate=0.00%


  █ TOTAL RESULTS

    checks_total.......: 180016  2998.993795/s
    checks_succeeded...: 100.00% 180016 out of 180016
    checks_failed......: 0.00%   0 out of 180016

    ✓ not 5xx
    ✓ not 429
    ✓ status 200
    ✓ final balance equals start balance

    HTTP
    http_req_duration..............: avg=3.92ms  med=3.77ms p(95)=7.08ms  p(99)=35.16ms max=87.56ms
      { expected_response:true }...: avg=3.92ms  med=3.77ms p(95)=7.08ms  p(99)=35.16ms max=87.56ms
    http_req_failed................: 0.00% 0 out of 60005
    http_reqs......................: 60005 999.659045/s

    EXECUTION
    dropped_iterations.............: 0     0/s
    iteration_duration.............: avg=12.12ms med=9.16ms p(95)=34.44ms p(99)=73.87ms max=141.09ms
    iterations.....................: 20001 333.208575/s
    vus............................: 3     min=2          max=24
    vus_max........................: 100   min=100        max=100

    NETWORK
    data_received..................: 11 MB 177 kB/s
    data_sent......................: 11 MB 184 kB/s




running (1m00.0s), 000/100 VUs, 20001 complete and 0 interrupted iterations
one_wallet ✓ [======================================] 000/100 VUs  1m0s  333.33 iters/s
```

</details>
