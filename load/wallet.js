// The load test: 1000 RPS on one wallet with no error status.
// make load seeds the wallet and gives BASE_URL and WALLET_ID.

import http from "k6/http";
import { check } from "k6";

const baseURL = __ENV.BASE_URL;
const walletID = __ENV.WALLET_ID;

// Each iteration sends 3 requests, so 1000 iterations in 3 s give 1000 RPS.
const requestsPerIteration = 3;

export const options = {
  scenarios: {
    one_wallet: {
      executor: "constant-arrival-rate",
      rate: 1000,
      timeUnit: `${requestsPerIteration}s`,
      duration: "60s",
      preAllocatedVUs: 100,
      maxVUs: 500,
    },
  },
  thresholds: {
    http_req_failed: ["rate==0"],
    checks: ["rate==1"],
    dropped_iterations: ["count==0"],
  },
  summaryTrendStats: ["avg", "med", "p(95)", "p(99)", "max"],
};

function getBalance() {
  const res = http.get(`${baseURL}/api/v1/wallet/${walletID}`, {
    tags: { name: "GET /api/v1/wallet/{walletId}" },
  });
  checkStatus(res);
  return res.status === 200 ? res.json("balance") : null;
}

function operation(operationType, amount) {
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

export function setup() {
  if (!baseURL || !walletID) {
    throw new Error("BASE_URL and WALLET_ID must be set. Run make load.");
  }
  const balance = getBalance();
  if (balance === null) {
    throw new Error(`Cannot read the balance of wallet ${walletID}. Run make load, which seeds the wallet.`);
  }
  return { startBalance: balance };
}

// The WITHDRAW cancels the DEPOSIT. A lost update changes the final balance.
// Other VUs run at the same time, so the requests of different iterations interleave on the row lock.
export default function () {
  const amount = 1 + Math.floor(Math.random() * 100);
  operation("DEPOSIT", amount);
  operation("WITHDRAW", amount);
  getBalance();
}

export function teardown(data) {
  const finalBalance = getBalance();
  check(finalBalance, {
    "final balance equals start balance": (b) => b === data.startBalance,
  });
  console.log(`start balance ${data.startBalance}, final balance ${finalBalance}`);
}
