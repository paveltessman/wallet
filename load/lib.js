// The request helpers of the load scenarios.

import http from "k6/http";
import { check } from "k6";

export const baseURL = __ENV.BASE_URL;

const getTags = { tags: { name: "GET /api/v1/wallet/{walletId}" } };

export function getBalance(walletID) {
  const res = http.get(`${baseURL}/api/v1/wallet/${walletID}`, getTags);
  checkStatus(res);
  return res.status === 200 ? res.json("balance") : null;
}

// The setup and the teardown read many wallets. http.batch sends the GETs of one chunk in parallel.
export function getBalances(walletIDs, chunkSize = 100) {
  const balances = [];
  for (let i = 0; i < walletIDs.length; i += chunkSize) {
    const requests = walletIDs
      .slice(i, i + chunkSize)
      .map((id) => ["GET", `${baseURL}/api/v1/wallet/${id}`, null, getTags]);
    for (const res of http.batch(requests)) {
      checkStatus(res);
      balances.push(res.status === 200 ? res.json("balance") : null);
    }
  }
  return balances;
}

export function operation(walletID, operationType, amount) {
  const res = http.post(
    `${baseURL}/api/v1/wallet`,
    JSON.stringify({ walletId: walletID, operationType, amount }),
    {
      headers: { "Content-Type": "application/json" },
      tags: { name: `POST /api/v1/wallet ${operationType}` },
    },
  );
  checkStatus(res);
}

// Separate checks show in the summary which rule failed.
function checkStatus(res) {
  check(res, {
    "not 5xx": (r) => r.status < 500,
    "not 429": (r) => r.status !== 429,
    "status 200": (r) => r.status === 200,
  });
}

export const thresholds = {
  http_req_failed: ["rate==0"],
  checks: ["rate==1"],
  dropped_iterations: ["count==0"],
};

export const summaryTrendStats = ["avg", "med", "p(95)", "p(99)", "max"];
