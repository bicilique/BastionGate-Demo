# Keep BastionGate credentials and release control server-side

Acme People integrates through a same-origin Go facade rather than calling BastionGate from browser JavaScript. This keeps API-client credentials out of the browser, lets the application enforce the Released File boundary before download, and prevents future UI changes from bypassing BastionGate's trust decision.

