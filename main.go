package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"
)

var (
	version     = "0.0.0" // Will be set during build
	showVersion = flag.Bool("version", false, "Show version information")
)

type SSHConfig struct {
	Keys     []StoredEntry
	Accounts []StoredEntry
	Hosts    []StoredEntry
	Jumps    []StoredEntry
}

type StoredEntry struct {
	Value     string    `json:"value"`
	LastUsed  time.Time `json:"last_used"`
	CreatedAt time.Time `json:"created_at"`
}

type StoredData struct {
	Keys     []StoredEntry `json:"keys"`
	Accounts []StoredEntry `json:"accounts"`
	Hosts    []StoredEntry `json:"hosts"`
	Jumps    []StoredEntry `json:"jumps"`
}

func getConfigPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".config", "wisshrd")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("could not create config directory: %w", err)
	}

	return filepath.Join(configDir, "history.json"), nil
}

func loadStoredData() (*StoredData, error) {
	data := &StoredData{
		Keys:     []StoredEntry{},
		Accounts: []StoredEntry{},
		Hosts:    []StoredEntry{},
		Jumps:    []StoredEntry{},
	}

	configPath, err := getConfigPath()
	if err != nil {
		return data, err
	}

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return data, nil
	}

	file, err := os.ReadFile(configPath)
	if err != nil {
		return data, fmt.Errorf("could not read config file: %w", err)
	}

	if err := json.Unmarshal(file, data); err != nil {
		return data, fmt.Errorf("could not parse config file: %w", err)
	}

	return data, nil
}

func saveStoredData(data *StoredData) error {
	configPath, err := getConfigPath()
	if err != nil {
		return err
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal config data: %w", err)
	}

	if err := os.WriteFile(configPath, jsonData, 0600); err != nil {
		return fmt.Errorf("could not write config file: %w", err)
	}

	return nil
}

func getValues(entries []StoredEntry) []string {
	values := make([]string, len(entries))
	for i, entry := range entries {
		values[i] = entry.Value
	}
	return values
}

func createStoredEntry(value string) StoredEntry {
	now := time.Now()
	return StoredEntry{
		Value:     value,
		LastUsed:  now,
		CreatedAt: now,
	}
}

func addOrUpdateEntry(entries []StoredEntry, value string) []StoredEntry {
	now := time.Now()
	for i, entry := range entries {
		if entry.Value == value {
			entries[i].LastUsed = now
			return entries
		}
	}
	return append(entries, StoredEntry{
		Value:     value,
		LastUsed:  now,
		CreatedAt: now,
	})
}

func loadSSHConfig() (*SSHConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	config := &SSHConfig{
		Keys:     []StoredEntry{},
		Accounts: []StoredEntry{},
		Hosts:    []StoredEntry{},
		Jumps:    []StoredEntry{},
	}

	// Add current user as the primary option for keys
	currentUser, err := user.Current()
	if err == nil {
		config.Keys = append(config.Keys, createStoredEntry(currentUser.Username))
	}

	// Read SSH config file for service users and hosts
	sshConfigPath := filepath.Join(homeDir, ".ssh", "config")
	if file, err := os.Open(sshConfigPath); err == nil {
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "Host ") {
				host := strings.TrimPrefix(line, "Host ")
				if !strings.Contains(host, "*") {
					for _, h := range strings.Fields(host) {
						config.Hosts = append(config.Hosts, createStoredEntry(h))
					}
				}
			} else if strings.HasPrefix(line, "User ") {
				user := strings.TrimPrefix(line, "User ")
				config.Accounts = append(config.Accounts, createStoredEntry(user))
			} else if strings.HasPrefix(line, "ProxyJump ") {
				jump := strings.TrimPrefix(line, "ProxyJump ")
				config.Jumps = append(config.Jumps, createStoredEntry(jump))
			}
		}
	}

	// Load stored data and merge appropriately
	storedData, err := loadStoredData()
	if err == nil {
		config.Keys = append(config.Keys, storedData.Keys...)
		config.Accounts = append(config.Accounts, storedData.Accounts...)
		config.Hosts = append(config.Hosts, storedData.Hosts...)
		config.Jumps = append(config.Jumps, storedData.Jumps...)
	}

	return config, nil
}

func runFzf(items []string, prompt string) (string, error) {
	args := []string{
		"--height", "20%",
		"--min-height", "1",
		"--print-query",
		"--no-margin",
		"--no-padding",
		"--prompt", fmt.Sprintf("%s (%d options) > ", prompt, len(items)),
	}
	cmd := exec.Command("fzf", args...)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}

	go func() {
		defer stdin.Close()
		for _, item := range items {
			fmt.Fprintln(stdin, item)
		}
	}()

	output, err := cmd.Output()
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if ok && exitErr.ExitCode() == 1 {
			lines := strings.Split(string(output), "\n")
			if len(lines) > 0 && lines[0] != "" {
				return lines[0], nil
			}
		}
		return "", err
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) >= 2 {
		selection := strings.TrimSpace(lines[1])
		if selection != "" {
			return selection, nil
		}
		return strings.TrimSpace(lines[0]), nil
	}

	return strings.TrimSpace(string(output)), nil
}

func promptConfirmation(sshCmd string) bool {
	fmt.Printf("\nConnect using: %s\nProceed? [y/N] ", sshCmd)
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(response) == "y"
}

func executeSSH(sshCmd string) error {
	cmd := exec.Command("ssh", sshCmd)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("wisshrd version %s\n", version)
		os.Exit(0)
	}

	config, err := loadSSHConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading SSH config: %v\n", err)
		os.Exit(1)
	}

	storedData, _ := loadStoredData()

	// Select key user (usually current user)
	key, err := runFzf(getValues(config.Keys), "key")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting key: %v\n", err)
		os.Exit(1)
	}
	if key != config.Keys[0].Value {
		storedData.Keys = addOrUpdateEntry(storedData.Keys, key)
	}

	// Select account (service users)
	account, err := runFzf(getValues(config.Accounts), "account")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting account: %v\n", err)
		os.Exit(1)
	}
	storedData.Accounts = addOrUpdateEntry(storedData.Accounts, account)

	// Select host
	host, err := runFzf(getValues(config.Hosts), "host")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting host: %v\n", err)
		os.Exit(1)
	}
	storedData.Hosts = addOrUpdateEntry(storedData.Hosts, host)

	// Select jump host
	jump, err := runFzf(getValues(config.Jumps), "jump")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error selecting jump host: %v\n", err)
		os.Exit(1)
	}
	if jump != "" {
		storedData.Jumps = addOrUpdateEntry(storedData.Jumps, jump)
	}

	// Save updated data
	saveStoredData(storedData)

	// Build the SSH command
	sshCmd := fmt.Sprintf("%s@%s@%s", key, account, host)
	if jump != "" {
		sshCmd = fmt.Sprintf("%s@%s", sshCmd, jump)
	}

	// Show the command and prompt for confirmation
	if promptConfirmation(sshCmd) {
		if err := executeSSH(sshCmd); err != nil {
			fmt.Fprintf(os.Stderr, "Error executing SSH command: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("Connection cancelled")
	}
}
