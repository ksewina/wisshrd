package main

import (
    "bufio"
    "encoding/json"
    "fmt"
    "os"
    "os/exec"
    "os/user"
    "path/filepath"
    "strings"
)

type SSHConfig struct {
    Keys     []string // local SSH key
    Accounts []string // service accounts 
    Hosts    []string
    Jumps    []string
}

type StoredData struct {
    Keys     []string `json:"keys"`     // Alternative key users
    Accounts []string `json:"accounts"` // Service accounts
    Hosts    []string `json:"hosts"`
    Jumps    []string `json:"jumps"`
}

func loadStoredData() (*StoredData, error) {
    data := &StoredData{
        Keys:     []string{},
        Accounts: []string{},
        Hosts:    []string{},
        Jumps:    []string{},
    }

    if _, err := os.Stat("history.json"); os.IsNotExist(err) {
        return data, nil
    }

    file, err := os.ReadFile("history.json")
    if err != nil {
        return data, nil
    }

    json.Unmarshal(file, data)
    return data, nil
}

func saveStoredData(data *StoredData) error {
    jsonData, err := json.MarshalIndent(data, "", "  ")
    if err != nil {
        return err
    }

    return os.WriteFile("history.json", jsonData, 0600)
}

func addUniqueToSlice(slice []string, item string) []string {
    for _, existing := range slice {
        if existing == item {
            return slice
        }
    }
    return append(slice, item)
}

func loadSSHConfig() (*SSHConfig, error) {
    homeDir, err := os.UserHomeDir()
    if err != nil {
        return nil, err
    }

    config := &SSHConfig{
        Keys:     []string{},
        Accounts: []string{},
        Hosts:    []string{},
        Jumps:    []string{},
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
                    config.Hosts = append(config.Hosts, strings.Fields(host)...)
                }
            } else if strings.HasPrefix(line, "User ") {
                user := strings.TrimPrefix(line, "User ")
                // Users from SSH config typically go to accounts list
                config.Accounts = append(config.Accounts, user)
            } else if strings.HasPrefix(line, "ProxyJump ") {
                jump := strings.TrimPrefix(line, "ProxyJump ")
                config.Jumps = append(config.Jumps, jump)
            }
        }
    }

    // Add current user as the primary option for keys
    currentUser, err := user.Current()
    if err == nil {
        config.Keys = append(config.Keys, currentUser.Username)
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

func runFzf(items []string) (string, error) {
    args := []string{"--height", "40%", "--print-query"}
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
        // If no selection was made but there was input, return the input
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
        // If a selection was made, return it; otherwise return the query
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
    config, err := loadSSHConfig()
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error loading SSH config: %v\n", err)
        os.Exit(1)
    }

    storedData, _ := loadStoredData()

    // Select key user (usually current user)
    fmt.Print("key > ")
    key, err := runFzf(config.Keys)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error selecting key: %v\n", err)
        os.Exit(1)
    }
    if key != config.Keys[0] { // Only store if it's not the current user
        storedData.Keys = addUniqueToSlice(storedData.Keys, key)
    }

    // Select account (service users)
    fmt.Print("account > ")
    account, err := runFzf(config.Accounts)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error selecting account: %v\n", err)
        os.Exit(1)
    }
    storedData.Accounts = addUniqueToSlice(storedData.Accounts, account)

    // Select host
    fmt.Print("host > ")
    host, err := runFzf(config.Hosts)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error selecting host: %v\n", err)
        os.Exit(1)
    }
    storedData.Hosts = addUniqueToSlice(storedData.Hosts, host)

    // Select jump host
    fmt.Print("jump > ")
    jump, err := runFzf(config.Jumps)
    if err != nil {
        fmt.Fprintf(os.Stderr, "Error selecting jump host: %v\n", err)
        os.Exit(1)
    }
    if jump != "" {
        storedData.Jumps = addUniqueToSlice(storedData.Jumps, jump)
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
