package scoring

import (
	"fmt"
	"strconv"
	"sync"
)

func ScoreImage() {
	// Ensure checks aren't blank, and grab TeamID.
	CheckConfigData()

	// If local is enabled, we want to:
	//    1. Score checks
	//    2. Check if server is up (if remote)
	//    3. If connection, report score
	//    4. Generate report
	if mc.Config.Local {
		scoreChecks()
		if mc.Config.Remote != "" {
			checkServer()
			if mc.Connection {
				reportScore()
			}
		}
		genReport(mc.Image)

		// If local is disabled, we want to:
		//    1. Check if server is up
		//    2. If no connection, generate report with err text
		//    3. If connection, score checks
		//    4. Report the score
		//    5. If reporting failed, show error, wipe scoring data
		//    6. Generate report
	} else {
		checkServer()
		if !mc.Connection {
			if VerboseEnabled {
				WarnPrint("Connection failed-- generating blank report.")
			}
			genReport(mc.Image)
			return
		}
		scoreChecks()
		err := reportScore()
		if err != nil {
			mc.Image = imageData{}
			if VerboseEnabled {
				WarnPrint("Local is disabled, scoring data removed.")
			}
		}
		genReport(mc.Image)
	}

	// Check if points increased/decreased
	prevPoints, err := ReadFile(mc.DirPath + "previous.txt")
	if err == nil {
		prevScore, _ := strconv.Atoi(prevPoints)
		if prevScore < mc.Image.Score {
			SendNotification("You gained points!")
			playAudio(mc.DirPath + "assets/gain.wav")
		} else if prevScore > mc.Image.Score {
			SendNotification("You lost points!")
			playAudio(mc.DirPath + "assets/alarm.wav")
		}
	} else {
		WarnPrint("Reading from previous.txt failed. This is probably fine.")
	}

	writeFile(mc.DirPath+"previous.txt", strconv.Itoa(mc.Image.Score))
}

// checkConfigData performs preliminary checks on the configuration data,
// and reads the TeamID file.
func checkConfigData() {
	if len(mc.Config.Check) == 0 {
		mc.Conn.OverallColor = "red"
		mc.Conn.OverallStatus = "There were no checks found in the configuration."
	} else {
		// For none-remote local connections
		mc.Conn.OverallColor = "green"
		mc.Conn.OverallStatus = "OK"
	}
	readTeamID()
}

// scoreChecks runs through every check configured and runs them concurrently.
func scoreChecks() {
	mc.Image = imageData{}
	assignPoints()

	var wg sync.WaitGroup
	for _, check := range mc.Config.Check {
		wg.Add(1)
		go scoreCheck(&wg, check)
	}

	wg.Wait()
	if VerboseEnabled {
		InfoPrint("Finished running all checks.")
	}

	if VerboseEnabled {
		InfoPrint(fmt.Sprintf("Score: %d", mc.Image.Score))
	}
}

// scoreCheck will go through each condition inside a check, and determine
// whether or not the check passes.
func scoreCheck(wg *sync.WaitGroup, check check) {
	defer wg.Done()
	status := true
	passStatus := []bool{}
	for i, condition := range check.Pass {
		passItemStatus := processCheckWrapper(&check, condition.Type, condition.Arg1, condition.Arg2, condition.Arg3)
		passStatus = append(passStatus, passItemStatus)
		if DebugEnabled {
			InfoPrint(fmt.Sprint("Result of last pass check was ", passStatus[i]))
		}
	}

	// For multiple pass conditions, will only be true if ALL of them are
	for _, result := range passStatus {
		status = status && result
		if !status {
			break
		}
	}
	if DebugEnabled {
		InfoPrint(fmt.Sprint("Result of all pass check was ", status))
	}

	// If a PassOverride succeeds, that overrides the Pass checks
	for _, condition := range check.PassOverride {
		passOverrideStatus := processCheckWrapper(&check, condition.Type, condition.Arg1, condition.Arg2, condition.Arg3)
		if DebugEnabled {
			InfoPrint(fmt.Sprint("Result of pass override was ", passOverrideStatus))
		}
		if passOverrideStatus {
			status = true
			break
		}
	}
	for _, condition := range check.Fail {
		failStatus := processCheckWrapper(&check, condition.Type, condition.Arg1, condition.Arg2, condition.Arg3)
		if DebugEnabled {
			InfoPrint(fmt.Sprint("Result of fail check was ", failStatus))
		}
		if failStatus {
			status = false
			break
		}
	}
	if check.Points >= 0 {
		if status {
			if VerboseEnabled {
				PassPrint(fmt.Sprintf("Check passed: %s - %d pts", check.Message, check.Points))
			}
			mc.Image.Points = append(mc.Image.Points, scoreItem{check.Message, check.Points})
			mc.Image.Score += check.Points
			mc.Image.Contribs += check.Points
		}
	} else {
		if status {
			if VerboseEnabled {
				FailPrint(fmt.Sprintf("Penalty triggered: %s - %d pts", check.Message, check.Points))
			}
			mc.Image.Penalties = append(mc.Image.Penalties, scoreItem{check.Message, check.Points})
			mc.Image.Score += check.Points
			mc.Image.Detracts += check.Points
		}
	}
}

// assignPoints is used to automatically assign points to checks that don't
// have a hardcoded points value.
func assignPoints() {
	pointlessChecks := []int{}

	for i, check := range mc.Config.Check {
		if check.Points == 0 {
			pointlessChecks = append(pointlessChecks, i)
			mc.Image.ScoredVulns++
		} else if check.Points > 0 {
			mc.Image.TotalPoints += check.Points
			mc.Image.ScoredVulns++
		}
	}

	pointsLeft := 100 - mc.Image.TotalPoints
	if pointsLeft <= 0 && len(pointlessChecks) > 0 || len(pointlessChecks) > 100 {
		// If the specified points already value over 100, yet there are
		// checks without points assigned, we assign the default point value
		// of 3 (arbitrarily chosen).
		for _, check := range pointlessChecks {
			mc.Config.Check[check].Points = 3
		}
	} else if pointsLeft > 0 && len(pointlessChecks) > 0 {
		pointsEach := pointsLeft / len(pointlessChecks)
		for _, check := range pointlessChecks {
			mc.Config.Check[check].Points = pointsEach
		}
		mc.Image.TotalPoints += (pointsEach * len(pointlessChecks))
		if mc.Image.TotalPoints < 100 {
			for i := 0; mc.Image.TotalPoints < 100; mc.Image.TotalPoints++ {
				mc.Config.Check[pointlessChecks[i]].Points++
				i++
				if i > len(pointlessChecks)-1 {
					i = 0
				}
			}
			mc.Image.TotalPoints += (100 - mc.Image.TotalPoints)
		}
	}
}
