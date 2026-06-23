// https://github.com/siyuan-note/siyuan/pull/8012
export const registerServiceWorker = (
    scriptURL: string,
    options: RegistrationOptions = {
        scope: "/",
        type: "classic",
        updateViaCache: "all",
    },
) => {
    void scriptURL;
    void options;

    /// #if BROWSER
    if (window.webkit?.messageHandlers || window.JSAndroid || window.JSHarmony ||
        !("serviceWorker" in window.navigator)
        || !("caches" in window)
        || !("fetch" in window)
        || navigator.serviceWorker == null
    ) {
        return;
    }

    window.navigator.serviceWorker.getRegistrations().then(registrations => {
        registrations.forEach(registration => {
            registration.unregister();
        });
    }).catch(e => {
        console.debug(`Unregister service worker failed with ${e}`);
    });
    /// #endif
};
