/** Admin password of the gateways started by global-setup.ts. */
export const ADMIN_PASSWORD = 'e2e-admin-password'

/**
 * Outbound proxy for every Kiro request the gateway might make: a closed local
 * port, so nothing can reach the real upstream and failures are immediate.
 */
export const DEAD_PROXY = 'http://127.0.0.1:9'

/** Environment variable through which global setup hands a value to the tests. */
export const envKey = (what: 'URL' | 'STATE' | 'LOG', project: string) => `KIRO_E2E_${what}_${project.toUpperCase()}`
