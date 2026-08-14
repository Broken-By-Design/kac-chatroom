// Creates the main application object if it doesn't exist.
var AdminPanel = window.AdminPanel || {};

document.addEventListener("DOMContentLoaded", function () {
    customCloak(
        "https://ssl.gstatic.com/docs/documents/images/kix-favicon-2023q4.ico",
        "Google Docs"
    );

    // initial run
    const iframe = document.getElementById("chatPreview");
    iframe.src = `https://${location.hostname}/student-portal`;

    const usersListElement = document.querySelector("#usersList ul");
    const selectedUsersTitle = document.querySelector(".section_container h1");
    const refreshBtn = document.getElementById("refreshBtn");
    const kickBtn = document.getElementById("kickBtn");
    const muteBtn = document.getElementById("muteBtn");
    const unmuteBtn = document.getElementById("unmuteBtn");


    // Message elements
    const successMessage = document.getElementById("successMessage");
    const failMessage = document.getElementById("failMessage");

    // Global commands
    const resetChatBtn = document.getElementById("resetChatBtn");
    const reloadAllUsersBtn = document.getElementById("reloadAllUsersBtn");
    const refreshBanListBtn = document.getElementById("refreshBanListBtn");
    const resetBotHistoryBtn = document.getElementById("resetBotHistoryBtn");
    const clearCacheBtn = document.getElementById("clearCacheBtn");

    // Troll Commands
    const jumpscareBtn = document.getElementById("jumpscareBtn");
    const crashBtn = document.getElementById("crashBtn");
    const censorBtn = document.getElementById("censorBtn");
    const uncensorBtn = document.getElementById("uncensorBtn");



    // Message-based ones
    const pinnedMessageForm = document.getElementById("pinnedMessageForm");
    const systemMessageForm = document.getElementById("systemMessageForm");
    const userMessageForm = document.getElementById("userMessageForm");

    const pinnedMessageInput = document.getElementById("pinnedMessageInput");

    const systemMessageInput = document.getElementById("systemMessageInput");
    const userMessageNameInput = document.getElementById(
        "userMessageNameInput"
    );
    const userMessageContentsInput = document.getElementById(
        "userMessageContentsInput"
    );

    let selectedUsers = [];

    // Function to show a message and then hide it
    const showMessage = (element, message, duration = 3000) => {
        element.textContent = message;
        element.style.display = "block";
        setTimeout(() => {
            element.style.display = "none";
        }, duration);
    };

    // Function to fetch users and render them
    const fetchAndRenderUsers = async () => {
        try {
            const response = await fetch("/get-users"); // Your backend endpoint
            if (!response.ok) {
                throw new Error("Network response was not ok");
            }
            const users = await response.json();
            // Clear the current list
            usersListElement.innerHTML = "";

            // Populate the list with users from the backend
            Object.entries(users).forEach((user) => {
                const userItem = document.createElement("li");
                userItem.innerHTML = `<label><input type="checkbox" data-username="${user[0]}" /> ${user[0]} (${user[1]})</label>`;
                usersListElement.appendChild(userItem);
            });
        } catch (error) {
            console.error("Failed to fetch users:", error);
            usersListElement.innerHTML = "<li>Failed to load users.</li>";
        }
    };

    // Function to update the list of selected users
    const updateSelectedUsers = () => {
        selectedUsers = [];
        const checkboxes = usersListElement.querySelectorAll(
            'input[type="checkbox"]:checked'
        );
        checkboxes.forEach((checkbox) => {
            selectedUsers.push(checkbox.dataset.username);
        });

        if (selectedUsers.length > 0) {
            selectedUsersTitle.textContent = `Selected: ${selectedUsers.join(
                ", "
            )}`;
        } else {
            selectedUsersTitle.textContent =
                "Select users to perform an action.";
        }
    };

    // Function to perform an action on selected users
    const performAction = async (action, details = {}) => {
        if (selectedUsers.length === 0) {
            showMessage(failMessage, "Please select at least one user.");
            return;
        }

        try {
            const response = await fetch(`/admin/${action}`, {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify({ users: selectedUsers, ...details }),
            });

            if (response.ok) {
                showMessage(successMessage, `Action '${action}' successful.`);
                fetchAndRenderUsers(); // Refresh the list after the action
            } else {
                const errorData = await response.json();
                showMessage(failMessage, `Error: ${errorData.message}`);
            }
        } catch (error) {
            console.error(`Failed to perform action '${action}':`, error);
            showMessage(failMessage, "An unexpected error occurred.");
        }
    };

    const performActionNoUser = async (action, details = {}) => {
        try {
            const response = await fetch(`/admin/${action}`, {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify(details),
            });

            if (response.ok) {
                showMessage(successMessage, `Action '${action}' successful.`);
                fetchAndRenderUsers(); // Refresh the list after the action
            } else {
                const errorData = await response.json();
                showMessage(failMessage, `Error: ${errorData.message}`);
            }
        } catch (error) {
            console.error(`Failed to perform action '${action}':`, error);
            showMessage(failMessage, "An unexpected error occurred.");
        }
    };

    const performActionOneUser = async (action, details = {}) => {
        if (selectedUsers.length > 1) {
            showMessage(failMessage, "Please only select one user.");
            return;
        }
        if (selectedUsers.length === 0) {
            showMessage(failMessage, "Please select a user.");
            return;
        }

        try {
            const response = await fetch(`/admin/${action}`, {
                method: "POST",
                headers: {
                    "Content-Type": "application/json",
                },
                body: JSON.stringify({ users: selectedUsers, ...details }),
            });

            if (response.ok) {
                showMessage(successMessage, `Action '${action}' successful.`);
                fetchAndRenderUsers(); // Refresh the list after the action
            } else {
                const errorData = await response.json();
                showMessage(failMessage, `Error: ${errorData.message}`);
            }
        } catch (error) {
            console.error(`Failed to perform action '${action}':`, error);
            showMessage(failMessage, "An unexpected error occurred.");
        }
    };

    systemMessageForm.addEventListener("submit", async function (event) {
        // 1. Prevent the default form submission which causes a page reload
        event.preventDefault();

        // 2. Get the message from the input field
        const message = systemMessageInput.value;

        // Basic validation: ensure the message is not empty
        if (!message.trim()) {
            showMessage(failMessage, "System message cannot be empty.");
            return; // Stop the function
        }

        performActionNoUser("system-message", { message: message });
    });

    userMessageForm.addEventListener("submit", async function (event) {
        // 1. Prevent the default form submission
        event.preventDefault();

        // 2. Get the values from the input fields
        const username = userMessageNameInput.value;
        const message = userMessageContentsInput.value;

        // Basic validation
        if (!username.trim() || !message.trim()) {
            showMessage(failMessage, "Username and message cannot be empty.");
            return; // Stop the function
        }

        performActionNoUser("user-message", {
            message: message,
            username: username,
        });
    });

    pinnedMessageForm.addEventListener("submit", async function (event) {
        event.preventDefault();

        const message = pinnedMessageInput.value;

        if (!message.trim()) {
            showMessage(failMessage, "Pinned message cannot be empty.");
            return;
        }

        performActionNoUser("pinned-message", { message: message });
    });

    // Event Listeners
    refreshBtn.addEventListener("click", fetchAndRenderUsers);
    usersListElement.addEventListener("change", updateSelectedUsers);

    // --- Wire up all your action buttons ---
    kickBtn.addEventListener("click", () => performAction("kick"));
    muteBtn.addEventListener("click", () => performAction("mute"));
    unmuteBtn.addEventListener("click", () => performAction("unmute"));

    document.getElementById("1dayBanBtn").addEventListener("click", () => {
        if (
            confirm(
                "This command is very dangerous and irreversible. Are you sure you want to proceed?"
            )
        ) {
            performAction("ban", { duration: "1d" });
        }
    });
    document.getElementById("1weekBanBtn").addEventListener("click", () => {
        if (
            confirm(
                "This command is very dangerous and irreversible. Are you sure you want to proceed?"
            )
        ) {
            performAction("ban", { duration: "7d" });
        }
    });
    document.getElementById("IPBanBtn").addEventListener("click", () => {
        if (
            confirm(
                "This command is very dangerous and irreversible. Are you sure you want to proceed?"
            )
        ) {
            if (confirm("Are you absolutely sure? This action is PERMANENT!")) {
                performAction("ip-ban");
            }
        }
    });

    jumpscareBtn.addEventListener("click", () => performAction("jumpscare"));

    crashBtn.addEventListener("click", () => performAction("crash-users"));

    censorBtn.addEventListener("click", () => performAction("censor-users"));
    uncensorBtn.addEventListener("click", () => performAction("uncensor-users"));


    // Global
    resetChatBtn.addEventListener("click", () =>
        performActionNoUser("reset-chat")
    );
    reloadAllUsersBtn.addEventListener("click", () =>
        performActionNoUser("reload-all")
    );
    refreshBanListBtn.addEventListener("click", () =>
        performActionNoUser("update-bans")
    );

    resetBotHistoryBtn.addEventListener("click", () => {
        performActionNoUser("system-message", {
            message: "*KAC-Bot's* history has been **reset** by and Admin!",
        });
        performActionNoUser("reset-bot-memory");
    });

    clearCacheBtn.addEventListener("click", () =>
        performActionNoUser("clear-cache")
    );

    // --- Live server memory usage ---
    const memStats = document.getElementById("memStats");

    const fetchMemStats = async () => {
        try {
            const response = await fetch("/admin/mem-stats");
            if (!response.ok) return;
            const data = await response.json();
            memStats.innerHTML = `
                <div class="mem-stat"><b>${data.rss_mb.toFixed(1)} MB</b><span>Process RSS</span></div>
                <div class="mem-stat"><b>${data.heap_mb.toFixed(1)} MB</b><span>Go Heap</span></div>
                <div class="mem-stat"><b>${data.sys_mb.toFixed(1)} MB</b><span>Go Sys</span></div>
                <div class="mem-stat"><b>${data.goroutines}</b><span>Goroutines</span></div>
            `;
        } catch (error) {
            memStats.textContent = "Unable to load memory stats.";
        }
    };
    fetchMemStats();
    setInterval(fetchMemStats, 5000);

    // --- YouTube stream credential health ---
    const streamCreds = document.getElementById("streamCreds");
    const resetCredHealthBtn = document.getElementById("resetCredHealthBtn");
    const testCredHealthBtn = document.getElementById("testCredHealthBtn");

    const esc = (s) =>
        String(s ?? "").replace(/[&<>"']/g, (ch) => ({
            "&": "&amp;",
            "<": "&lt;",
            ">": "&gt;",
            '"': "&quot;",
            "'": "&#39;",
        }[ch]));

    const fetchStreamCreds = async () => {
        try {
            const response = await fetch("/admin/stream-creds");
            if (!response.ok) return;
            const data = await response.json();
            if (!data.length) {
                streamCreds.innerHTML =
                    '<p class="panel-sub">No credentials configured.</p>';
                return;
            }
            streamCreds.innerHTML = data
                .map((p) => {
                    const badge =
                        p.flagged && p.status === "failed"
                            ? '<span class="cred-badge cred-bad">BAD - replace</span>'
                            : p.status === "ok"
                              ? '<span class="cred-badge cred-good">OK</span>'
                              : p.status === "failed"
                                ? '<span class="cred-badge cred-warn">Failing</span>'
                                : '<span class="cred-badge cred-standby">Standby (not probed)</span>';
                    const when = p.last_tested
                        ? new Date(p.last_tested).toLocaleTimeString()
                        : "never";
                    return `<div class="cred-row">
                        <div><b>${esc(p.label)}</b> ${badge}</div>
                        <div class="cred-detail">cookies: ${
                            p.cookies_file ? esc(p.cookies_file) : "none"
                        } | PO token: ${p.has_po_token ? "yes" : "no"} | streak: ${
                            p.fail_streak
                        } | last tested: ${when}</div>
                        ${
                            p.last_error
                                ? `<div class="cred-error">${esc(p.last_error)}</div>`
                                : ""
                        }
                    </div>`;
                })
                .join("");
        } catch (error) {
            streamCreds.textContent = "Unable to load credential health.";
        }
    };
    fetchStreamCreds();
    setInterval(fetchStreamCreds, 10000);

    resetCredHealthBtn.addEventListener("click", async () => {
        try {
            const response = await fetch("/admin/stream-creds/reset", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({}),
            });
            if (response.ok) {
                showMessage(successMessage, "Credential health flags reset.");
                fetchStreamCreds();
            } else {
                const errorData = await response.json();
                showMessage(failMessage, `Error: ${errorData.message}`);
            }
        } catch (error) {
            showMessage(failMessage, "An unexpected error occurred.");
        }
    });

    testCredHealthBtn.addEventListener("click", async () => {
        showMessage(successMessage, "Probing credentials…");
        try {
            const response = await fetch("/admin/stream-creds/test", {
                method: "POST",
            });
            if (response.ok) {
                fetchStreamCreds();
            } else {
                const errorData = await response.json();
                showMessage(failMessage, `Error: ${errorData.message}`);
            }
        } catch (error) {
            showMessage(failMessage, "An unexpected error occurred.");
        }
    });

    // Initial load
    fetchAndRenderUsers();
});
