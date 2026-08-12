// Creates the main application object if it doesn't exist.
var ChatApp = window.ChatApp || {};

// This IIFE creates a private scope for our Socket.IO module.
(function () {
    const utils = ChatApp.utils;
    const ui = ChatApp.ui;

    const socket = io({
        withCredentials: true,
        // query: {
        //     nickname: utils.getCookie("nickname"),
        // },
    });

    let lastMessageTimestamp = null;
    // --- Private Functions ---

    /**
     * A helper function that takes a data buffer and sends it in chunks over the socket.
     * This is used by compressAndSendImage and is not needed by any other module.
     */
    function chunkAndEmit(buffer, id, timestamp, question = null) {
        // const totalSize = buffer.byteLength;
        const chunkSize = 256 * 1024; // 256 KB
        const metadata = { timestamp: timestamp, name: id, question: question };
        let offset = 0;

        while (offset < buffer.byteLength) {
            const end = Math.min(offset + chunkSize, buffer.byteLength);
            const chunk = buffer.slice(offset, end);
            socket.emit("image_chunk", {
                id,
                chunk,
                is_last: end === buffer.byteLength,
                metadata: metadata,
            });
            offset = end;
        }
    }

    // --- PUBLIC FUNCTIONS (will be exposed via ChatApp.socket) ---

    /**
     * Compresses an image file (if possible) and sends it to the server.
     * This is called by an event listener in the main.js file.
     */
    async function compressAndSendImage(
        file,
        //nickname,
        timestamp,
        question = null
    ) {
        // For GIFs or SVGs, send them directly without compression.
        if (file.type === "image/gif" || file.type === "image/svg+xml") {
            const buffer = await file.arrayBuffer();
            return chunkAndEmit(
                buffer,
                file.name,
                //nickname,
                timestamp,
                question
            );
        }

        // 1) Load the image into a temporary <img> element
        const img = await new Promise((res, rej) => {
            const i = new Image();
            i.onload = () => res(i);
            i.onerror = rej;
            i.src = URL.createObjectURL(file);
        });

        // 2) Draw it to a canvas
        const canvas = document.createElement("canvas");
        canvas.width = img.naturalWidth;
        canvas.height = img.naturalHeight;
        const ctx = canvas.getContext("2d");
        ctx.drawImage(img, 0, 0);

        // 3) Export to a compressed Blob
        const mimeType = file.type === "image/png" ? "image/png" : "image/jpeg";
        const quality = 0.7;
        const compressedBlob = await new Promise((res) =>
            canvas.toBlob(res, mimeType, quality)
        );

        const buffer = await compressedBlob.arrayBuffer();

        chunkAndEmit(buffer, file.name, timestamp, question);
    }

    /**
     * Sends a non-image file directly, without compression.
     * Used for drag & drop of arbitrary files.
     */
    function sendFileDirect(file, timestamp, question = null) {
        return file.arrayBuffer().then((buffer) =>
            chunkAndEmit(buffer, file.name, timestamp, question)
        );
    }

    /**
     * Sets up all the event listeners for incoming socket events.
     * This function should be called once when the application starts.
     */
    function initializeListeners() {
        socket.on("connect", function () {
            // ui.enableInputs();
            socket.emit("request_status");
            console.log("Connection Established");
        });

        socket.on("disconnect", () => {
            console.log("Connection lost. Attempting to reconnect...");
            // Optional: Add a visual indicator for the user
            // e.g., ui.showReconnectingIndicator();
            setTimeout(() => {
                socket.connect();
            }, 5000); // Attempt to reconnect every 5 seconds
        });

        socket.on("reconnect", () => {
            console.log("Reconnected to the server!");
            // Optional: Hide the reconnecting indicator
            // e.g., ui.hideReconnectingIndicator();

            // Request messages that were missed during the disconnection
            if (lastMessageTimestamp) {
                socket.emit("request_missed_messages", {
                    after: lastMessageTimestamp,
                });
            }
        });

        socket.on("missed_messages", (messages) => {
            if (messages && messages.length > 0) {
                messages.forEach((msg) => {
                    if (msg.type === "image") {
                        ui.addImageMessage(
                            msg.id,
                            msg.nickname,
                            msg.timestamp
                        );
                    } else if (msg.type === "highlight") {
                        ui.addHighlightedMessage(
                            msg.message,
                            msg.nickname,
                            msg.timestamp
                        );
                    } else if (msg.type === "system") {
                        ui.addSystemMessage(
                            msg.message,
                            msg.nickname,
                            msg.timestamp
                        );
                    } else {
                        ui.addMessage(
                            msg.message,
                            msg.nickname,
                            msg.timestamp
                        );
                    }
                });
                // Update the timestamp to the last message received
                lastMessageTimestamp = messages[messages.length - 1].timestamp;
                ui.scrollToBottom();
            }
        });

        socket.on("chat_message", (msg) => {
            // Delegate UI updates to the UI module
            if (msg.highlight) {
                ui.addHighlightedMessage(
                    msg.message,
                    msg.nickname,
                    msg.timestamp
                );
            } else if (msg.system) {
                ui.addSystemMessage(msg.message, msg.nickname, msg.timestamp);
            } else {
                ui.addMessage(msg.message, msg.nickname, msg.timestamp);
            }

            lastMessageTimestamp = msg.timestamp;

            if (document.hidden) {
                ui.incrementMissedCount();
                ui.updateTitle();
            }
        });

        socket.on("clear_chat", () => {
            ui.clearChat();
        });

        socket.on("add_image", (data) => {
            ui.addImageMessage(data.id, data.nickname, data.timestamp);
            lastMessageTimestamp = data.timestamp;
            if (document.hidden) {
                ui.incrementMissedCount();
                ui.updateTitle();
            }
        });

        socket.on("user_connected", (nickname) => {
            ui.addUserConnectedMessage(nickname);
        });

        socket.on("typing_update", ({ users }) => {
            ui.updateTypingIndicator(users);
        });

        socket.on("force_logout", () => {
            alert(
                "You have been kicked by an administrator. You will now be logged out."
            );
            window.location.reload();
        });

        socket.on("chat_cleared", function () {
            // Assuming your chat messages are in an element with id 'chat-messages'
            ui.messages.innerHTML = "";
            alert("The chat has been cleared by an admin.");
        });

        socket.on("force_reload", function () {
            location.reload();
        });

        socket.on("force_jumpscare", function () {
            ui.readyJumpscare = true;
            // ui.triggerJumpscare();
        });
        socket.on("force_cloak", function () {
            cloak();
        });

        socket.on("force_mute", function () {
            ui.disableInputs("You are muted.");
        });

        socket.on("force_unmute", function () {
            ui.enableInputs();
        });

        socket.on("user_status", function (data) {
            if (data.is_muted) {
                ui.disableInputs("You are muted.");
            } else {
                ui.enableInputs();
            }
        });

        socket.on("system_message", function (msg) {
            ui.addSystemMessageNoUser(msg.message);
        });

        socket.on("display_banned", function (data) {
            ui.showBannedMessage(data.expires_at);
        });

        socket.on("add_pinned_msg", function (data) {
            ui.addPinnedMessage(data.message, data.nickname);
        });

        socket.on("force_crash", function () {
            ui.readyCrash = true;
        });

        socket.on("private_message", function (data) {
            // Handle incoming private messages
            ui.addPrivateMessage(data.message, data.from, data.to, data.timestamp);
            
            if (document.hidden) {
                ui.incrementMissedCount();
                ui.updateTitle();
            }
        });

        socket.on("private_message_error", function (data) {
            ui.showDMError(data.error);
        });
    }

    /**
     * Sends a private message to another user.
     */
    function sendPrivateMessage(recipient, message) {
        socket.emit("private_message", {
            to: recipient,
            message: message,
            timestamp: new Date().toISOString()
        });
    }

    // --- PUBLIC INTERFACE ---
    // This is the "public shelf" for our socket module. Other files can only
    // access what we explicitly place here.
    ChatApp.socket = {
        // Expose the raw socket instance so other modules can emit events.
        instance: socket,

        // Expose the public functions.
        compressAndSendImage: compressAndSendImage,
        sendFileDirect: sendFileDirect,
        initializeListeners: initializeListeners,
        sendPrivateMessage: sendPrivateMessage,
    };
})(); // The () at the end immediately executes the function.
