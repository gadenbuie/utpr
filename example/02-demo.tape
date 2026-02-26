# utpr demo
#
# Assumes:
#   - utpr is on PATH
#   - 01-setup.sh has been run (creates /tmp/utpr-demo and /tmp/utpr-demo.env)
#   - gh, git, gum are on PATH and authenticated
#
# Record with: vhs example/02-demo.tape   (run from the repo root)

Output example/demo.gif

Set Shell        bash
Set FontSize     15
Set Width        1100
Set Height       550
Set TypingSpeed  70ms
Set Theme        "GitHub Dark"
Env COLORTERM    truecolor

# --- Hidden setup: navigate to the demo repo and set a clean prompt ---
Hide
Type "source /tmp/utpr-demo.env"
Enter
Sleep 500ms
Type "cd /tmp/utpr-demo"
Enter
Sleep 500ms
Type "PS1='\[\033[0;32m\]utpr-demo\[\033[0m\] \[\033[0;34m\]$\[\033[0m\] ' && clear"
Enter
Sleep 200ms
Show

# --- Look at what we're starting with ---
Type "cat README.md"
Enter
Sleep 1.5s

# --- Start a new PR branch via the issue picker ---
Enter
Type "# start a new feature branch"
Enter
Sleep 400ms
Type "utpr init"
Enter
Sleep 3s      # gum filter loads with open issues
Enter         # select issue #1 "Add project status"
Sleep 1.5s   # "Preview issue #1?" confirm appears (default=No)
Left          # show preview
Sleep 1
Enter
Sleep 3
Type q
Sleep 1.5s   # "Branch name:" input appears, pre-filled with fix/1-add-project-status
Enter         # accept suggested branch name
Sleep 1.5s   # "Assign yourself?" confirm appears (default=Yes)
Enter         # assign and continue
Sleep 3s

# --- Make a change ---
Enter
Sleep 1
Type "# make a change to the README"
Enter
Sleep 400ms
Type "echo '## Status' >> README.md"
Enter
Sleep 200ms
Type "echo '' >> README.md"
Enter
Sleep 150ms
Type "echo 'Active and welcoming contributions.' >> README.md"
Enter
Sleep 200ms

# --- Commit the change ---
Enter
Type "# commit the work"
Enter
Sleep 400ms
Type "git add README.md && git commit -m 'docs: add status section'"
Enter
Sleep 1s

# --- Push the branch, then drive the gh pr create interactive form ---
# gh pr create: title (prefilled) → body "enter to skip" → action menu
Enter
Type "# push and open a pull request"
Enter
Sleep 400ms
Type "utpr push"
Enter
Sleep 1.5s   # git push completes; gh pr create title prompt appears
Enter        # accept the prefilled title
Sleep 1s     # prompt: "? Body [(e) to launch vim, enter to skip]"
Enter        # skip body
Sleep 2s     # "What's next?" menu appears (Submit is default)
Enter        # Submit
Sleep 3s

# --- Pause work and return to main ---
Enter
Sleep 3s
Type "# pause your work"
Enter
Sleep 400ms
Type "utpr pause"
Enter
Sleep 3s

# --- Resume: gum filter shows branches sorted by committerdate; fix/1-add-project-status is first ---
Enter
Type "# pick up where you left off"
Enter
Sleep 400ms
Type "utpr resume"
Enter
Sleep 2s
Enter
Sleep 1.5s

# --- Go back to main ---
Enter
Type "# head back to main"
Enter
Sleep 400ms
Type "utpr pause"
Enter
Sleep 2s

# --- Clean up after a merged PR via the PR picker ---
Enter
Type "# clean up after a merged PR"
Enter
Sleep 400ms
Type "utpr finish"
Enter
Sleep 2.5s   # gum filter loads with merged PRs that have a local branch
Enter        # select PR #2 (feat/add-contributing)
Sleep 1.5s   # "Finish PR #2 (branch: feat/add-contributing)?" confirm appears
Enter        # confirm
Sleep 3s

Sleep 1.5s
