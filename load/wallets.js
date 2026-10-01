// The load test: 1000 RPS spread over many wallets with no error status.
// Each iteration picks a random wallet, so the batches are small and many wallet workers run at one time.
// make load-wallets seeds the wallets and gives BASE_URL and WALLET_COUNT.

import { check } from "k6";
import { baseURL, getBalance, getBalances, operation, summaryTrendStats, thresholds } from "./lib.js";

const walletCount = parseInt(__ENV.WALLET_COUNT, 10);

// Each iteration sends 3 requests, so 1000 iterations in 3 s give 1000 RPS.
const requestsPerIteration = 3;

export const options = {
  scenarios: {
    random_wallets: {
      executor: "constant-arrival-rate",
      rate: 1000,
      timeUnit: `${requestsPerIteration}s`,
      duration: "60s",
      preAllocatedVUs: 100,
      maxVUs: 500,
    },
  },
  thresholds,
  summaryTrendStats,
};

// load/seed-wallets.sql makes the same IDs.
function walletID(n) {
  return `00000000-0000-4000-9000-${String(n).padStart(12, "0")}`;
}

function allWalletIDs() {
  return Array.from({ length: walletCount }, (_, i) => walletID(i + 1));
}

function sum(balances) {
  return balances.reduce((a, b) => a + b, 0);
}

export function setup() {
  if (!baseURL || !(walletCount > 0)) {
    throw new Error("BASE_URL and WALLET_COUNT must be set. Run make load-wallets.");
  }
  const balances = getBalances(allWalletIDs());
  const missing = balances.filter((b) => b === null).length;
  if (missing > 0) {
    throw new Error(`Cannot read ${missing} of ${walletCount} wallets. Run make load-wallets, which seeds the wallets.`);
  }
  // The setup data goes to every VU, so it holds the sum and not one balance for each wallet.
  return { startSum: sum(balances) };
}

// The WITHDRAW cancels the DEPOSIT, so a lost update changes the sum of the balances.
export default function () {
  const id = walletID(1 + Math.floor(Math.random() * walletCount));
  const amount = 1 + Math.floor(Math.random() * 100);
  operation(id, "DEPOSIT", amount);
  operation(id, "WITHDRAW", amount);
  getBalance(id);
}

export function teardown(data) {
  const balances = getBalances(allWalletIDs());
  const finalSum = balances.includes(null) ? null : sum(balances);
  check(finalSum, {
    "final sum equals start sum": (s) => s === data.startSum,
  });
  console.log(`start sum ${data.startSum}, final sum ${finalSum}`);
}
