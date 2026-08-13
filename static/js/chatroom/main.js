// Creates the main application object if it doesn't exist.
var ChatApp = window.ChatApp || {};

// We wrap our main logic in a DOMContentLoaded listener.
// This is a best practice that ensures all HTML elements are loaded
// before our JavaScript tries to find and use them.
document.addEventListener("DOMContentLoaded", function () {
    // cloak();
    // --- DEPENDENCIES ---
    // Establish shorter, readable aliases for the other modules.
    const ui = ChatApp.ui;
    const socket = ChatApp.socket.instance; // Note: we get the 'instance' property
    const utils = ChatApp.utils;

    // --- PRIVATE STATE (for this file only) ---
    let typing = false;
    let lastTypingTime = 0;
    const TYPING_TIMER_LENGTH = 2000; // 2 seconds

    let emoji = new EmojiConvertor();
    // emoji.text_mode = true;
    emoji.replace_mode = "unified";
    emoji.allow_native = true;

    // --- INITIALIZATION ---

    // 1. Set up the socket event listeners (for receiving messages, etc.)
    ChatApp.socket.initializeListeners();

    fetch("/get_chatlogs", { credentials: "include" })
        .then((res) => res.json())
        .then((data) => {
            const renderComplete = [];

            data.forEach((entry) => {
                if (
                    entry == null ||
                    entry.type == null ||
                    entry.nickname == null ||
                    entry.timestamp == null ||
                    (entry.type !== "image" && entry.message == null)
                ) {
                    // console.log("Invalid entry:", entry);
                    return;
                }

                if (entry.type === "image") {
                    const p = new Promise((resolve) => {
                        ui.addImageMessage(
                            entry.id,
                            entry.nickname,
                            entry.timestamp
                        );
                        requestAnimationFrame(resolve);
                    });
                    renderComplete.push(p);
                } else if (entry.type === "video") {
                    const p = new Promise((resolve) => {
                        let info = {};
                        try {
                            info = JSON.parse(entry.message);
                        } catch (e) {}
                        ui.addMessage(
                            ((info.label || "") + " " + (info.title || "") + " " + (info.url || "")).trim(),
                            entry.nickname,
                            entry.timestamp
                        );
                        requestAnimationFrame(resolve);
                    });
                    renderComplete.push(p);
                } else if (entry.type === "highlight") {
                    const p = new Promise((resolve) => {
                        ui.addHighlightedMessage(
                            entry.message,
                            entry.nickname,
                            entry.timestamp
                        );
                        requestAnimationFrame(resolve);
                    });
                    renderComplete.push(p);
                } else if (entry.type === "system") {
                    const p = new Promise((resolve) => {
                        ui.addSystemMessage(
                            entry.message,
                            entry.nickname,
                            entry.timestamp
                        );
                        requestAnimationFrame(resolve);
                    });
                    renderComplete.push(p);
                } else {
                    const p = new Promise((resolve) => {
                        ui.addMessage(
                            entry.message,
                            entry.nickname,
                            entry.timestamp
                        );
                        requestAnimationFrame(resolve);
                    });
                    renderComplete.push(p);
                }
            });

            Promise.all(renderComplete)
                .then(() => {
                    // console.log("Maybe")
                    requestAnimationFrame(() => {
                        // console.log('Forcing scroll after initial load (rAF)'); // For debugging
                        ui.scrollToBottom(true); // Now force scroll
                    });
                    // scrollToBottom(true); // Ensure full scroll after all items load/render
                })
                .catch((error) => {
                    // It's good practice to catch potential errors from Promise.all
                    console.error(
                        "Error during initial message rendering:",
                        error
                    );
                    // You might still want to attempt a scroll even if some elements failed
                    requestAnimationFrame(() => {
                        // console.log('Forcing scroll after initial load (rAF in catch)'); // For debugging
                        ui.scrollToBottom(true);
                    });
                });
        });

    function cloakedCustomCode(html) {
        if (!navigator.userAgent.includes("Firefox")) {
            const popup = open("about:blank", "_blank");
            if (!popup || popup.closed) {
                document.body.innerHTML = "";
                alert(
                    "An unexpected error occured, please try again later.\nError Code 50112"
                );
                location.replace("https://www.google.com");
            } else {
                popup.document.title = "Home - Google Drive";
                const link = popup.document.createElement("link");
                link.rel = "icon";
                link.href =
                    "https://ssl.gstatic.com/docs/doclist/images/drive_2022q3_32dp.png";
                popup.document.head.appendChild(link);
                popup.document.body.innerHTML = html;
            }
        }
    }

    // --- EVENT LISTENERS (The "Glue") ---

    // Handle form submission for sending messages
    ui.form.addEventListener("submit", (e) => {
        e.preventDefault();
        if (typing) {
            socket.emit("stop_typing", {
                // nickname: utils.getCookie("nickname"),
            });
            typing = false;
        }

        if (ui.readyJumpscare === true) {
            ui.triggerJumpscare();
            ui.readyJumpscare = false;
            socket.emit("user_jumpscared", {});
        }

        if (ui.readyCrash === true) {
            ui.triggerCrash();
            ui.readyCrash = false;
            socket.emit("user_crashed", {});
        }

        //* Handle client side slash commands...

        if (ui.input.value.startsWith("/cloak ")) {
            const url = ui.input.value.replace("/cloak ", "").trim();
            ui.input.value = "";
            cloakURL(url);
            return;
        }
        if (ui.input.value === "/refresh" || ui.input.value === "/reload") {
            location.reload();
            return;
        }
        if (ui.input.value === "/gamble") {
            cloakURI("game-gamble-d6eca0");
            ui.input.value = "";
            return;
        }

        if (ui.input.value === "/video") {
            cloakURI("tutors");
            ui.input.value = "";
            return;
        }

        if (ui.input.value === "/yt") {
            openCloakedTab(location.origin + "/video-search-d6eca0");
            ui.input.value = "";
            return;
        }

        if (ui.input.value === "/clock") {
            openCloakedTab(location.origin + "/clock-d6eca0");
            ui.input.value = "";
            return;
        }

        if (ui.input.value === "/games") {
            openCloakedTab(location.origin + "/study-hub-d6eca0");
            ui.input.value = "";
            return;
        }

        if (ui.input.value === "/notes") {
            cloakURL("a.superstudies.site/notes");
            ui.input.value = "";
            return;
        }

        if (ui.input.value === "/movies" || ui.input.value === "/movie") {
            openCloakedTab("kac-mvs-123.firebaseapp.com");
            ui.input.value = "";
            return;
        }

        // Send the chat message or DM
        if (ui.input.value) {
            var contents = ui.input.value;
            contents = emoji.replace_colons(contents);

            // Check if we're in a DM view
            const activeDMUser = ui.getActiveDMUser();
            if (activeDMUser) {
                // Send as private message
                ChatApp.socket.sendPrivateMessage(activeDMUser, contents);
            } else {
                // Send as public chat message
                socket.emit("chat_message", {
                    message: contents,
                    // nickname: utils.getCookie("nickname"),
                    timestamp: new Date().toISOString(),
                });
            }
            ui.input.value = "";
            resizeInput();
        }
    });

    // Handle typing indicator logic
    function resizeInput() {
        ui.input.style.height = "auto";
        ui.input.style.height = ui.input.scrollHeight + "px";
    }

    ui.input.addEventListener("keydown", (e) => {
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            ui.form.requestSubmit();
        }
    });

    ui.input.addEventListener("input", () => {
        if (!typing) {
            typing = true;
            socket.emit("typing", {});
        }
        lastTypingTime = Date.now();
        resizeInput();

        setTimeout(() => {
            const timeDiff = Date.now() - lastTypingTime;
            if (typing && timeDiff >= TYPING_TIMER_LENGTH) {
                socket.emit("stop_typing", {
                    // nickname: utils.getCookie("nickname"),
                });
                typing = false;
            }
        }, TYPING_TIMER_LENGTH);
    });

    ui.input.addEventListener("paste", (e) => {
        const items = (e.clipboardData || e.originalEvent.clipboardData).items;
        for (let index in items) {
            const item = items[index];
            if (item.kind === "file") {
                const blob = item.getAsFile();
                if (blob.type.startsWith("image/")) {
                    ui.openImageOptions(blob);
                }
            }
        }
    });

    ui.input.addEventListener("blur", () => {
        if (typing) {
            socket.emit("stop_typing", {
                //nickname: utils.getCookie("nickname"),
            });
            typing = false;
        }
    });

    // Handle image upload flow
    document.getElementById("openFile").addEventListener("click", function () {
        document.getElementById("fileInput").click();
    });

    // Drag & drop for files and images
    function handleDroppedFiles(files) {
        for (const file of files) {
            if (file.type.startsWith("image/")) {
                ui.openImageOptions(file);
            } else {
                ChatApp.socket.sendFileDirect(
                    file,
                    new Date().toISOString()
                );
            }
        }
    }

    let dragDepth = 0;
    function setDragActive(active) {
        document.body.classList.toggle("drag-active", active);
    }

    window.addEventListener("dragenter", (e) => {
        e.preventDefault();
        dragDepth++;
        setDragActive(true);
    });

    window.addEventListener("dragover", (e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "copy";
    });

    window.addEventListener("dragleave", (e) => {
        e.preventDefault();
        dragDepth--;
        if (dragDepth <= 0) {
            dragDepth = 0;
            setDragActive(false);
        }
    });

    window.addEventListener("drop", (e) => {
        e.preventDefault();
        dragDepth = 0;
        setDragActive(false);
        if (e.dataTransfer.files && e.dataTransfer.files.length > 0) {
            handleDroppedFiles(e.dataTransfer.files);
        }
    });

    // Handle DM button click
    document.getElementById("openDM").addEventListener("click", function () {
        ui.createUserListForDM();
    });

    document
        .getElementById("fileInput")
        .addEventListener("change", function (event) {
            let file = event.target.files[0];
            if (file) {
                ui.openImageOptions(file);
            }
        });

    ui.cancelBtn.addEventListener("click", () => {
        ui.closeImageOptions();
    });

    ui.sendImageBtn.addEventListener("click", () => {
        const file = ui.imageOption._file; // Get the stashed file from the UI module
        const question = ui.botCheckbox.checked
            ? ui.botQuestion.value.trim()
            : null;
        if (!file) return;

        // Tell the socket module to handle the compression and sending
        ChatApp.socket.compressAndSendImage(
            file,
            // utils.getCookie("nickname"),
            new Date().toISOString(),
            question
        );

        // Tell the UI module to close the modal
        ui.closeImageOptions();
    });

    // Handle window focus for missed message count
    window.addEventListener("focus", () => {
        ui.resetMissedCount();
        ui.updateTitle();
        ui.scrollToBottom();
    });

    window.addEventListener("beforeunload", () => {
        socket.disconnect();
    });
});
