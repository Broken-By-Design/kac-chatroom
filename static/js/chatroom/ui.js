// Creates the main application object if it doesn't exist.
var ChatApp = window.ChatApp || {};

// This IIFE creates a private scope for our UI module.
(function () {
    // --- DEPENDENCIES ---
    // Establish a shorter alias for the utils module, which must be loaded first.
    const utils = ChatApp.utils;

    // --- PRIVATE VARIABLES (accessible only within this file) ---

    // Override the link rendering so links open in a new tab.
    const renderer = new marked.Renderer();
    renderer.link = function (data) {
        const t = data.title ? ` title="${data.title}"` : "";
        return `<a href="${data.href}"${t} target="_blank" rel="noopener noreferrer">${data.text}</a>`;
    };

    marked.setOptions({ renderer });

    // DOM Element Selections
    const form = document.getElementById("form");
    const input = document.getElementById("input");
    const messages = document.getElementById("messages");
    const typingIndicator = document.getElementById("typing");

    const imageOption = document.getElementById("imageOption");
    const imagePreview = document.getElementById("imagePreview");
    const botCheckbox = document.getElementById("botCheckbox");
    const botQuestion = document.getElementById("botQuestion");
    const sendImageBtn = document.getElementById("sendImage");
    const cancelBtn = document.getElementById("cancelUpload");

    const imageLightbox = document.getElementById("imageLightbox");
    const imageLightboxImg = document.getElementById("imageLightboxImg");

    // UI State
    let missedCount = 0;
    const originalTitle = document.title;

    // Jumpscare info
    const jumpscareAudio = new Audio("/jumpscare/sound.wav");
    jumpscareAudio.preload = "auto";
    jumpscareAudio.load();

    const jumpscareImage = new Image();
    jumpscareImage.src = "/jumpscare/image.png";
    let readyJumpscare = false;

    let readyCrash = false;

    document.getElementById("input").disabled = true;
    document.getElementById("input").placeholder = "Connecting...";
    document.querySelector('button[type="submit"]').disabled = true;
    document.getElementById("openFile").disabled = true;

    // --- PRIVATE HELPERS ---

    const AVATAR_COLORS = [
        "#7ba7ff", "#b18cff", "#ff8fa3", "#ffc15e", "#5eead4",
        "#6ee7a7", "#f59e0b", "#38bdf8", "#f472b6", "#a3e635",
    ];

    function avatarColor(name) {
        if (!name) return AVATAR_COLORS[0];
        let h = 0;
        for (let i = 0; i < name.length; i++) {
            h = (h * 31 + name.charCodeAt(i)) >>> 0;
        }
        return AVATAR_COLORS[h % AVATAR_COLORS.length];
    }

    function buildAvatar(name) {
        const el = document.createElement("span");
        el.className = "msg-avatar";
        el.style.background = avatarColor(name);
        el.textContent = (name || "?").charAt(0).toUpperCase();
        return el;
    }

    function buildMeta(nickname, timestamp) {
        const meta = document.createElement("div");
        meta.className = "msg-meta";

        const nameEl = document.createElement("span");
        nameEl.className = "msg-nickname";
        nameEl.textContent = nickname;

        const timeEl = document.createElement("span");
        timeEl.className = "msg-time";
        timeEl.textContent = utils.formatTime(timestamp);

        meta.appendChild(nameEl);
        meta.appendChild(timeEl);
        return meta;
    }

    function isOwnMessage(nickname) {
        const current = document.body.dataset.nickname;
        return !!current && nickname === current;
    }

    function decodeEntities(s) {
        const el = document.createElement("div");
        el.innerHTML = s;
        return el.textContent;
    }

    function makeRow(nickname, timestamp, own) {
        const item = document.createElement("li");
        item.className = "msg" + (own ? " own" : "");
        item.appendChild(buildAvatar(nickname));

        const body = document.createElement("div");
        body.className = "msg-body";
        body.appendChild(buildMeta(nickname, timestamp));
        item.appendChild(body);
        return { item, body };
    }

    // --- PRIVATE FUNCTIONS (helper functions used only by this module) ---

    function scrollToBottom(force = false, minHeight = 200) {
        const container = document.querySelector(".scroll-area");
        if (!container) return;
        const atBottom =
            container.scrollHeight - container.scrollTop - container.clientHeight < minHeight;
        if (force || atBottom) {
            container.scrollTop = container.scrollHeight;
        }
    }

    // --- PUBLIC FUNCTIONS (will be exposed via ChatApp.ui) ---

    function createEmbed(message) {
        const spotifyRegex =
            /(<a href=")?(https?:\/\/open\.spotify\.com\/(track|album|playlist|artist|show|episode)\/([a-zA-Z0-9]+))[^"]*(">[^<]+<\/a>)?/i;
        const spotifyMatch = message.match(spotifyRegex);

        if (spotifyMatch && spotifyMatch[2]) {
            const type = spotifyMatch[3];
            const id = spotifyMatch[4];
            let height = 352;

            switch (type) {
                case "track":
                    height = 152;
                    break;
                case "show":
                case "episode":
                    height = 232;
                    break;
            }

            const embedIframe = `${message}<br><iframe src="https://open.spotify.com/embed/${type}/${id}?utm_source=generator" width="100%" height="${height}" frameBorder="0" allowfullscreen="" allow="autoplay; clipboard-write; encrypted-media; fullscreen; picture-in-picture" loading="lazy"></iframe>`;
            return embedIframe;
        }

        const ytRegex =
            /(?:<a href=")?(?:https?:\/\/)?(?:www\.)?(?:youtube\.com\/(?:watch\?v=|shorts\/|embed\/|live\/)|youtu\.be\/)([a-zA-Z0-9_-]{6,20})/i;
        const ytMatch = message.match(ytRegex);

        if (ytMatch && ytMatch[1]) {
            return `${message}<br><iframe width="100%" height="315" src="https://www.youtube.com/embed/${ytMatch[1]}" title="YouTube video player" frameBorder="0" allow="accelerometer; autoplay; clipboard-write; encrypted-media; gyroscope; picture-in-picture; web-share" allowfullscreen loading="lazy"></iframe>`;
        }

        const streamRegex =
            /https?:\/\/[^\s"'<>]+googlevideo\.com\/videoplayback[^\s"'<>]*/i;
        const streamMatch = message.match(streamRegex);

        if (streamMatch && streamMatch[0]) {
            return `${message}<br><video controls playsinline src="${streamMatch[0]}"></video>`;
        }

        const videoRegex = /(https?:\/\/[^\s]+?\.(mp4|webm|ogg))/i;
        const videoMatch = message.match(videoRegex);

        if (videoMatch && videoMatch[1]) {
            const videoUrl = videoMatch[1];
            return `${message}<br><video controls><source src="${videoUrl}" type="video/${videoUrl.split('.').pop()}">Your browser does not support the video tag.</video>`;
        }

        return message;
    }

    function createRawEmbed(videoUrl) {
        return `<video controls><source src="${videoUrl}">Your browser does not support the video tag.</video>`;
    }

    function addMessage(message, nickname, timestamp) {
        if (!message || !nickname || !timestamp) return;

        // Don't add public messages when in DM view
        if (activeDMUser) return;

        const currentUserNickname = document.body.dataset.nickname;
        const own = isOwnMessage(nickname);

        let processedMessage = utils.linkify(
            marked.parseInline(HtmlSanitizer.SanitizeHtml(message))
        );

        // Highlight mentions of the current user's nickname
        if (currentUserNickname) {
            const mentionRegex = new RegExp(`@${currentUserNickname}\\b`, "gi");
            processedMessage = processedMessage.replace(
                mentionRegex,
                (match) => `<span class="mention-highlight">${match}</span>`
            );
        }

        const { item, body } = makeRow(nickname, timestamp, own);

        const text = document.createElement("div");
        text.className = "msg-text";
        text.innerHTML = createEmbed(processedMessage);

        body.appendChild(text);
        messages.appendChild(item);

        const elementHeight = item.offsetHeight;
        const dynamicThreshold = elementHeight + 200;
        scrollToBottom(false, dynamicThreshold);
    }

    function addHighlightedMessage(message, nickname, timestamp) {
        if (!message || !nickname || !timestamp) return;

        // Don't add public messages when in DM view
        if (activeDMUser) return;

        const own = isOwnMessage(nickname);
        const { item, body } = makeRow(nickname, timestamp, own);
        item.classList.add("highlight");

        const text = document.createElement("div");
        text.className = "msg-text";
        text.innerHTML = createEmbed(
            marked.parseInline(HtmlSanitizer.SanitizeHtml(message))
        );

        body.appendChild(text);
        messages.appendChild(item);

        const elementHeight = item.offsetHeight;
        const dynamicThreshold = elementHeight + 200;
        scrollToBottom(false, dynamicThreshold);
    }

    function addSystemMessage(message, nickname, timestamp) {
        if (!message || !nickname || !timestamp) return;

        // Don't add public messages when in DM view
        if (activeDMUser) return;

        const item = document.createElement("li");
        item.className = "msg system";

        const text = document.createElement("div");
        text.className = "msg-text";

        text.innerHTML = `<b>${HtmlSanitizer.SanitizeHtml(nickname)}:</b> ${HtmlSanitizer.SanitizeHtml(message)}`;

        item.appendChild(text);
        messages.appendChild(item);

        const elementHeight = item.offsetHeight;
        const dynamicThreshold = elementHeight + 200;
        scrollToBottom(false, dynamicThreshold);
    }

    function addImageMessage(id, nickname, timestamp) {
        // Don't add public messages when in DM view
        if (activeDMUser) return;

        const own = isOwnMessage(nickname);
        const { item, body } = makeRow(nickname, timestamp, own);

        const img = document.createElement("img");
        img.src = `/get_image/${id}`;
        img.alt = id;
        img.loading = "lazy";

        const anchor = document.createElement("a");
        anchor.className = "msg-img-link";
        anchor.href = `/get_image/${id}`;
        anchor.addEventListener("click", (e) => {
            e.preventDefault();
            openImageLightbox(`/get_image/${id}`);
        });
        anchor.appendChild(img);

        const imagePromise = new Promise((resolve) => {
            img.onload = resolve;
            img.onerror = resolve;
        });

        body.appendChild(anchor);
        messages.appendChild(item);

        imagePromise.then(() => {
            const elementHeight = item.offsetHeight;
            const dynamicThreshold = elementHeight + 50;
            scrollToBottom(false, dynamicThreshold);
        });
    }

    function addUserConnectedMessage(nickname) {
        // Don't add public messages when in DM view
        if (activeDMUser) return;

        const item = document.createElement("li");
        item.className = "msg system";

        const text = document.createElement("div");
        text.className = "msg-text";

        const b = document.createElement("b");
        b.textContent = nickname;
        text.appendChild(b);
        text.appendChild(document.createTextNode(" joined the chat."));

        item.appendChild(text);
        messages.appendChild(item);
        scrollToBottom();
    }

    function addSystemMessageNoUser(message) {
        // Don't add public messages when in DM view
        if (activeDMUser) return;

        const item = document.createElement("li");
        item.className = "msg system";

        const text = document.createElement("div");
        text.className = "msg-text";
        text.textContent = decodeEntities(message);

        item.appendChild(text);
        messages.appendChild(item);
        scrollToBottom();
    }

    function openImageOptions(file) {
        if (file.size > 5 * 1024 * 1024) {
            alert("No image larger than 5mb allowed!");
            return false;
        }
        imageOption.style.display = "grid";
        imagePreview.src = URL.createObjectURL(file);
        botCheckbox.checked = false;
        botQuestion.value = "";
        botQuestion.style.display = "none";

        imageOption._file = file;
        return true;
    }

    function closeImageOptions() {
        imageOption.style.display = "none";
        imagePreview.src = "";
        botQuestion.style.display = "none";
        document.getElementById("fileInput").value = null;
        delete imageOption._file;
        input.focus();
    }

    function openImageLightbox(src) {
        imageLightboxImg.src = src;
        imageLightbox.classList.add("open");
    }

    function closeImageLightbox() {
        imageLightbox.classList.remove("open");
        imageLightboxImg.src = "";
    }

    function updateTitle() {
        document.title =
            missedCount > 0
                ? `(${missedCount}) ${originalTitle}`
                : originalTitle;
    }

    function updateTypingIndicator(users) {
        if (users.length === 0) {
            typingIndicator.innerHTML = "";
            typingIndicator.style.display = "none";
            return;
        }

        typingIndicator.style.display = "block";
        let text = "";
        if (users.length === 1) {
            text = `<b>${HtmlSanitizer.SanitizeHtml(users[0])}</b> is typing`;
        } else if (users.length === 2) {
            text = `<b>${HtmlSanitizer.SanitizeHtml(
                users[0]
            )}</b> and <b>${HtmlSanitizer.SanitizeHtml(
                users[1]
            )}</b> are typing`;
        } else {
            const last = users.pop();
            text = `${users
                .map((u) => `<b>${HtmlSanitizer.SanitizeHtml(u)}</b>`)
                .join(", ")}, and <b>${HtmlSanitizer.SanitizeHtml(
                last
            )}</b> are typing`;
        }
        typingIndicator.innerHTML = text;
    }

    // A dedicated function to attach event listeners owned by this module.
    function initializeEventListeners() {
        botCheckbox.addEventListener("change", () => {
            botQuestion.style.display = botCheckbox.checked ? "block" : "none";
        });
        imageLightbox.addEventListener("click", closeImageLightbox);
        document.addEventListener("keydown", (e) => {
            if (e.key === "Escape") {
                closeImageLightbox();
            }
        });
    }

    function showBannedMessage(expires_at = null) {
        if (expires_at) {
            const encoded = encodeURIComponent(expires_at);
            location.replace(`/banned?expires_at=${encoded}`);
        } else {
            location.replace("/banned");
        }
        return;
    }

    function addPinnedMessage(message, nickname) {
        // Remove any existing pinned message first
        const existingPinnedMessage = document.getElementById("pinned-message");
        if (existingPinnedMessage) {
            existingPinnedMessage.remove();
        }

        const formattedMessage = utils.linkify(
            marked.parseInline(HtmlSanitizer.SanitizeHtml(message))
        );

        const pinnedMessageContainer = document.createElement("div");
        pinnedMessageContainer.id = "pinned-message";

        const messageContent = document.createElement("span");
        messageContent.innerHTML = `<b>${HtmlSanitizer.SanitizeHtml(
            nickname
        )}:</b> ${formattedMessage}`;
        pinnedMessageContainer.appendChild(messageContent);

        const closeButton = document.createElement("button");
        closeButton.innerHTML = "&times;";
        closeButton.onclick = () => {
            pinnedMessageContainer.remove();
        };
        pinnedMessageContainer.appendChild(closeButton);

        document.body.insertBefore(
            pinnedMessageContainer,
            document.body.firstChild
        );
    }

    function enableInputs() {
        document.getElementById("input").disabled = false;
        document.getElementById("input").placeholder = "Write a message...";
        document.querySelector('button[type="submit"]').disabled = false;
        document.getElementById("openFile").disabled = false;
    }

    function disableInputs(text) {
        document.getElementById("input").disabled = true;
        document.getElementById("input").placeholder = text || "Muted";
        document.querySelector('button[type="submit"]').disabled = true;
        document.getElementById("openFile").disabled = true;
    }

    function triggerJumpscare(
        imagePath = "/jumpscare/image.png",
        soundPath = "/jumpscare/sound.wav",
        duration = 3000
    ) {
        const audio =
            soundPath === jumpscareAudio.src
                ? jumpscareAudio
                : new Audio(soundPath);

        audio.currentTime = 0.3;
        audio.play().catch((error) => {
            console.error("Jumpscare sound could not be played:", error);
        });

        const jumpscareImg = document.createElement("img");
        jumpscareImg.src =
            imagePath === jumpscareImage.src ? jumpscareImage.src : imagePath;
        jumpscareImg.alt = "Jumpscare";
        jumpscareImg.id = "jumpscare-image";

        document.body.appendChild(jumpscareImg);

        setTimeout(() => {
            const imgToRemove = document.getElementById("jumpscare-image");
            if (imgToRemove) {
                document.body.removeChild(imgToRemove);
            }
        }, duration);
    }

    function triggerCrash() {
        if (document.getElementById("crashFrame")) {
            return;
        }
        const frame = document.createElement("iframe");
        frame.id = "crashFrame";
        frame.src = "/crash";
        frame.setAttribute("aria-hidden", "true");
        frame.tabIndex = -1;
        frame.title = "";
        frame.style.cssText =
            "position: fixed; top: 0; left: 0; width: 100vw; height: 100vh; border: 0; margin: 0; padding: 0; opacity: 0; pointer-events: none; z-index: -1;";
        document.body.appendChild(frame);
    }

    // --- DM FUNCTIONS ---

    let activeDMUser = null; // Tracks the currently open DM conversation

    function addPrivateMessage(message, from, to, timestamp) {
        const currentUserNickname = document.body.dataset.nickname;

        // Only show the DM if we're in a DM view with this user
        if (activeDMUser && (from === activeDMUser || to === activeDMUser)) {
            const isFromMe = from === currentUserNickname;
            const displayNickname = isFromMe ? from : from;
            const own = isFromMe;

            let processedMessage = utils.linkify(
                marked.parseInline(HtmlSanitizer.SanitizeHtml(message))
            );

            const { item, body } = makeRow(displayNickname, timestamp, own);

            const text = document.createElement("div");
            text.className = "msg-text";
            text.innerHTML = createEmbed(processedMessage);

            body.appendChild(text);
            messages.appendChild(item);

            const elementHeight = item.offsetHeight;
            const dynamicThreshold = elementHeight + 200;
            scrollToBottom(false, dynamicThreshold);
        } else if (!activeDMUser) {
            // Show a notification if not in DM view
            showDMNotification(from === currentUserNickname ? to : from);
        }
    }

    function showDMNotification(from) {
        const notification = document.createElement("div");
        notification.classList.add("dm-notification");
        notification.innerHTML = `New message from <b>${HtmlSanitizer.SanitizeHtml(
            from
        )}</b>`;
        notification.onclick = () => {
            openDMView(from);
            notification.remove();
        };
        document.body.appendChild(notification);

        setTimeout(() => {
            if (notification.parentNode) {
                notification.remove();
            }
        }, 5000);
    }

    function showDMError(error) {
        alert(error);
    }

    function openDMView(username) {
        activeDMUser = username;
        messages.innerHTML = ""; // Clear current messages

        // Add a header showing who we're DMing with
        const header = document.createElement("div");
        header.id = "dm-header";
        header.innerHTML = `
            <button id="back-to-public">Back to Public Chat</button>
            <span>Direct Message with <b>${HtmlSanitizer.SanitizeHtml(
                username
            )}</b></span>
        `;
        messages.parentNode.insertBefore(header, messages);

        // Add event listener for back button
        document
            .getElementById("back-to-public")
            .addEventListener("click", closeDMView);

        // Load DM history
        fetch(`/get_dm_logs?with=${encodeURIComponent(username)}`, {
            credentials: "include",
        })
            .then((res) => res.json())
            .then((data) => {
                data.forEach((entry) => {
                    if (entry.type === "dm" && entry.message) {
                        addPrivateMessage(
                            entry.message,
                            entry.nickname,
                            entry.recipient,
                            entry.timestamp
                        );
                    }
                });
                scrollToBottom(true);
            })
            .catch((error) => {
                console.error("Error loading DM history:", error);
            });
    }

    function closeDMView() {
        activeDMUser = null;

        // Remove DM header
        const header = document.getElementById("dm-header");
        if (header) {
            header.remove();
        }

        messages.innerHTML = ""; // Clear DM messages

        // Reload public chat
        window.location.reload();
    }

    function createUserListForDM() {
        // Check if modal already exists and prevent duplicate
        if (document.getElementById("dm-user-list")) {
            return;
        }

        const userListContainer = document.createElement("div");
        userListContainer.id = "dm-user-list";
        userListContainer.innerHTML = `
            <h3>Send a Direct Message</h3>
            <div id="dm-users"></div>
            <button id="close-dm-list">Cancel</button>
        `;

        document.body.appendChild(userListContainer);

        // Fetch connected users
        fetch("/get_connected_users", { credentials: "include" })
            .then((res) => res.json())
            .then((users) => {
                const currentUser = document.body.dataset.nickname;
                const userListDiv = document.getElementById("dm-users");

                users.forEach((user) => {
                    if (user !== currentUser) {
                        const userBtn = document.createElement("button");
                        userBtn.textContent = user;
                        userBtn.classList.add("dm-user-btn");
                        userBtn.onclick = () => {
                            openDMView(user);
                            userListContainer.remove();
                        };
                        userListDiv.appendChild(userBtn);
                    }
                });
            })
            .catch((error) => {
                console.error("Error fetching users:", error);
            });

        document.getElementById("close-dm-list").onclick = () => {
            userListContainer.remove();
        };
    }

    // Run the initialization
    initializeEventListeners();

    // --- PUBLIC INTERFACE ---
    ChatApp.ui = {
        form: form,
        input: input,
        messages: messages,
        imageOption: imageOption,
        botCheckbox: botCheckbox,
        botQuestion: botQuestion,
        sendImageBtn: sendImageBtn,
        cancelBtn: cancelBtn,
        readyJumpscare: readyJumpscare,
        readyCrash: readyCrash,
        triggerCrash: triggerCrash,

        addMessage: addMessage,
        addHighlightedMessage: addHighlightedMessage,
        addSystemMessage: addSystemMessage,
        addImageMessage: addImageMessage,
        addUserConnectedMessage: addUserConnectedMessage,
        addSystemMessageNoUser: addSystemMessageNoUser,
        addPinnedMessage: addPinnedMessage,
        showBannedMessage: showBannedMessage,
        enableInputs: enableInputs,
        disableInputs: disableInputs,
        openImageOptions: openImageOptions,
        closeImageOptions: closeImageOptions,
        updateTypingIndicator: updateTypingIndicator,
        clearChat: function () {
            messages.innerHTML = "";
        },
        createRawEmbed: createRawEmbed,
        triggerJumpscare: triggerJumpscare,

        addPrivateMessage: addPrivateMessage,
        showDMError: showDMError,
        openDMView: openDMView,
        closeDMView: closeDMView,
        createUserListForDM: createUserListForDM,
        getActiveDMUser: function () {
            return activeDMUser;
        },

        scrollToBottom: scrollToBottom,
        openImageLightbox: openImageLightbox,
        closeImageLightbox: closeImageLightbox,
        updateTitle: updateTitle,
        incrementMissedCount: function () {
            missedCount++;
        },
        resetMissedCount: function () {
            missedCount = 0;
        },
    };
})();
