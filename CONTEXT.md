# BastionGate Secure Upload Demo

Acme People is a fictional profile application used to demonstrate that ordinary file uploads remain untrusted until BastionGate makes a final trust decision.

## Language

**Untrusted Upload**:
A file selected or submitted by a user that is not yet permitted to become application content.
_Avoid_: Uploaded avatar, pending avatar

**Accepted Submission**:
An Untrusted Upload that BastionGate has accepted for asynchronous processing. Acceptance is not evidence that the file is clean.
_Avoid_: Successful upload, safe upload

**Trust Decision**:
BastionGate's final determination of whether an Untrusted Upload is released, blocked, requires review, or failed processing.
_Avoid_: Scan result

**Blocked File**:
An Untrusted Upload that BastionGate prevents from being released to Acme People.
_Avoid_: Rejected avatar

**Released File**:
A file that BastionGate has explicitly made available after its Trust Decision. Only a Released File may become a profile photo.
_Avoid_: Clean upload

**EICAR Demo File**:
The harmless, industry-standard antivirus test file generated for the malicious-path demonstration.
_Avoid_: Virus, webshell, exploit payload

