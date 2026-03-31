#!/bin/bash
# Mock worker that immediately writes OUTPUT.md and exits successfully.
# The working directory is set to the task dir by cowork (c.Dir = taskDir),
# so writing to $PWD writes directly into the task directory.
set -e
echo "# Task Output

Mock worker completed successfully." > "${PWD}/OUTPUT.md"
exit 0
