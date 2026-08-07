var ChatApp = window.ChatApp || {};

(function () {
    /**
     * Retrieves a cookie value by its name.
     * @param {string} name - The name of the cookie to retrieve.
     * @returns {string|null} The cookie value, or null if not found.
     */
    function getCookie(name) {
        const cookies = document.cookie.split(";");
        for (let cookie of cookies) {
            let [key, value] = cookie.split("=");
            if (key && key.trim() === name) {
                return value;
            }
        }
        return null;
    }

    /**
     * Converts URLs and Markdown-style links in a string to HTML <a> tags.
     * @param {string} text - The input text to linkify.
     * @returns {string} The text with HTML links.
     */
    function linkify(text) {
      return text;
        const urlRegex =
            /(?:(?:https?|ftp):\/\/)?(?:www\.)?[-a-zA-Z0-9@:%._\+~#=]{1,256}\.[a-zA-Z0-9()]{1,6}\b(?:[-a-zA-Z0-9()@:%_\+.~#?&//=]*)/gi;

        return text.replace(urlRegex, (url) => {
            if (url.startsWith("(") || url.endsWith(")")) {
                return url; // don't turn it into a link
            }

            const href = /^(?:https?|ftp):\/\//i.test(url)
                ? url
                : `http://${url}`;
            return `<a href="${href}" target="_blank" rel="noopener noreferrer">${url}</a>`;
        });
    }

    /**
     * Formats an ISO date string into a localized time string (e.g., "3:45 PM").
     * @param {string} dateString - The ISO date string to format.
     * @returns {string} The formatted time.
     */
    function formatTime(dateString) {
        const date = new Date(dateString);
        return date.toLocaleTimeString("en-US", {
            hour: "numeric",
            minute: "numeric",
            hour12: true,
        });
    }

    ChatApp.utils = {
        getCookie: getCookie,
        linkify: linkify,
        formatTime: formatTime,
    };
})();
