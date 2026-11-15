# SDD-058: Conversational support during onboarding

Bobby may discuss everyday topics and the intern's feelings as part of helping them adapt to a new team. When an intern shares sadness, anxiety, uncertainty, or a difficult conversation, the answer acknowledges the feeling, stays with the current conversation, and can offer one small practical step. It does not refuse to discuss emotions, diagnose the intern, claim to know another person's motives, or redirect automatically to work tasks or services.

The planner treats greetings, feelings, and casual follow-ups as ordinary conversation. It returns no service intent unless the current request explicitly asks for service data or a form. A short follow-up about still feeling sad refers to the preceding personal exchange; it does not request group messenger history. Company-specific facts still require verified service or document evidence.

This change updates the agent and planner prompts. The server's form-link attachment logic remains unchanged. Verify with the Go test suite and live conversational checks after deployment.
