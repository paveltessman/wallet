// The load test: 1000 RPS on one wallet with no error status.
// make load seeds the wallet and gives BASE_URL and WALLET_ID.

import { check } from "k6";
import { baseURL, getBalance, operation, summaryTrendStats, thresholds } from "./lib.js";

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
  thresholds,
  summaryTrendStats,
};

export function setup() {
  if (!baseURL || !walletID) {
    throw new Error("BASE_URL and WALLET_ID must be set. Run make load.");
  }
  const balance = getBalance(walletID);
  if (balance === null) {
    throw new Error(`Cannot read the balance of wallet ${walletID}. Run make load, which seeds the wallet.`);
  }
  return { startBalance: balance };
}

// The WITHDRAW cancels the DEPOSIT. A lost update changes the final balance.
// Other VUs run at the same time, so the requests of different iterations interleave on the row lock.
export default function () {
  const amount = 1 + Math.floor(Math.random() * 100);
  operation(walletID, "DEPOSIT", amount);
  operation(walletID, "WITHDRAW", amount);
  getBalance(walletID);
}

export function teardown(data) {
  const finalBalance = getBalance(walletID);
  check(finalBalance, {
    "final balance equals start balance": (b) => b === data.startBalance,
  });
  console.log(`start balance ${data.startBalance}, final balance ${finalBalance}`);
}
