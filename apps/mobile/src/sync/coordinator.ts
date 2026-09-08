/**
 * Share an asynchronous operation while the same keyed operation is already
 * running. The owner is held weakly so a database that is discarded by the
 * provider does not stay alive because of the coordinator.
 */
const inFlight = new WeakMap<object, Map<string, Promise<unknown>>>();

export function singleFlight<T>(owner: object, key: string, task: () => Promise<T>): Promise<T> {
  let operations = inFlight.get(owner);
  if (!operations) {
    operations = new Map();
    inFlight.set(owner, operations);
  }

  const existing = operations.get(key) as Promise<T> | undefined;
  if (existing) return existing;

  const operation = task();
  operations.set(key, operation);
  const clear = () => {
    if (operations?.get(key) === operation) operations.delete(key);
  };
  operation.then(clear, clear);
  return operation;
}
