pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import QtQuick.Window 2.15
import "../themes"

Page {
    id: timelinePage

    property var hostWindow: null
    property var api: null
    property bool active: false
    property string startDate: ""
    property string endDate: ""
    property string jobId: ""
    property string jobState: "idle"
    property string errorMessage: ""
    property var result: null
    property var geodataStatus: null
    property int processedObservations: 0
    property int submissionSequence: 0
    property string submissionId: ""
    property string resultStartDate: ""
    property string resultEndDate: ""
    property string submittedStartDate: ""
    property string submittedEndDate: ""
    property bool resultRequested: false
    property bool statusRequestPending: false
    property int resultRetryCount: 0
    readonly property int maxResultRetries: 3
    property int currentView: 0
    property alias detailsButton: detailsToggle
    property alias timelineView: timelineViewContent
    property alias detailsPanel: detailsOverlay
    property alias quitButton: timelineQuitButton
    readonly property int currentUTCYear: new Date().getUTCFullYear()
    readonly property int selectedYear: {
        var year = Number(startDate.substring(0, 4));
        return isFinite(year) && year >= 1 ? year : currentUTCYear;
    }

    signal backRequested
    signal yearRequested(int year)
    signal quitRequested

    padding: 0
    background: Rectangle {
        color: theme.background
    }

    ThemeLoader {
        id: theme
    }

    function timelinePeriod() {
        return startDate === endDate ? startDate : startDate + " to " + endDate;
    }

    function showPlaceOnTimeline(place) {
        var stopIndex = timelineViewContent.stopIndexForPlace(place);
        if (stopIndex < 0)
            return;
        currentView = 0;
        timelineViewContent.showStop(stopIndex, place);
        Qt.callLater(timelineViewContent.forceActiveFocus);
    }

    function beginTimeline() {
        if (!api || startDate === "" || endDate === "") {
            errorMessage = "Choose a valid date range.";
            jobState = "failed";
            return;
        }
        if (jobId !== "" && (jobState === "queued" || jobState === "running" || jobState === "cancelling"))
            api.cancelTimeline(jobId);
        submissionSequence += 1;
        submissionId = Date.now().toString() + "-" + submissionSequence.toString();
        submittedStartDate = startDate;
        submittedEndDate = endDate;
        jobId = "";
        jobState = "submitting";
        errorMessage = "";
        result = null;
        currentView = 0;
        resultRequested = false;
        statusRequestPending = false;
        resultRetryCount = 0;
        processedObservations = 0;
        api.submitTimeline(startDate, endDate, submissionId);
    }

    function requestTimelineStatus(includeResult) {
        if (!api || jobId === "" || statusRequestPending)
            return;
        statusRequestPending = true;
        resultRequested = includeResult === true;
        api.getTimeline(jobId, resultRequested);
    }

    function retryCompletedResult(message) {
        resultRequested = false;
        resultRetryCount += 1;
        if (resultRetryCount >= maxResultRetries) {
            errorMessage = message + " Select Retry result to try again.";
            return;
        }
        errorMessage = message + " Retrying.";
        resultRetryTimer.interval = Math.min(8000, 1000 * Math.pow(2, resultRetryCount - 1));
        if (active && jobState === "completed")
            resultRetryTimer.restart();
    }

    function leaveTimeline() {
        cancelActiveTimeline();
        currentView = 0;
        submissionId = "";
        backRequested();
    }

    function cancelActiveTimeline() {
        if (!api || jobId === "" || (jobState !== "queued" && jobState !== "running"))
            return;
        jobState = "cancelling";
        api.cancelTimeline(jobId);
    }

    function invalidateCachedTimeline() {
        cancelActiveTimeline();
        resultRetryTimer.stop();
        submissionId = "";
        jobId = "";
        jobState = "idle";
        result = null;
        resultStartDate = "";
        resultEndDate = "";
        resultRequested = false;
        statusRequestPending = false;
        resultRetryCount = 0;
        errorMessage = "";
    }

    onActiveChanged: {
        if (active && jobState === "completed" && !result && jobId !== "" && submittedStartDate === startDate && submittedEndDate === endDate)
            requestTimelineStatus(true);
        else if (active && !(jobState === "completed" && result && resultStartDate === startDate && resultEndDate === endDate))
            beginTimeline();
        else if (!active)
            cancelActiveTimeline();
    }

    Connections {
        target: timelinePage.api
        enabled: timelinePage.api !== null

        function onTimelineSubmitted(requestId, id) {
            if (!timelinePage.active || requestId !== timelinePage.submissionId) {
                timelinePage.api.cancelTimeline(id);
                return;
            }
            timelinePage.jobId = id;
            timelinePage.jobState = "queued";
            timelinePage.requestTimelineStatus(false);
        }

        function onTimelineSubmitFailed(requestId, message) {
            if (!timelinePage.active || requestId !== timelinePage.submissionId)
                return;
            timelinePage.jobState = "unavailable";
            timelinePage.errorMessage = message;
            timelinePage.api.getGeodataStatus();
        }

        function onTimelineStatusFetched(id, status) {
            if (id !== timelinePage.jobId)
                return;
            var includedResult = timelinePage.resultRequested;
            timelinePage.statusRequestPending = false;
            if (!status || typeof status !== "object") {
                timelinePage.resultRequested = false;
                timelinePage.errorMessage = "Timeline status response was invalid.";
                return;
            }
            if ((timelinePage.jobState === "completed" || timelinePage.jobState === "failed" || timelinePage.jobState === "cancelled")
                    && status.state !== timelinePage.jobState)
                return;
            timelinePage.jobState = status.state || "failed";
            timelinePage.errorMessage = status.error || "";
            timelinePage.processedObservations = status.processed_observations || 0;
            if (status.result) {
                timelinePage.resultRequested = false;
                timelinePage.resultRetryCount = 0;
                timelinePage.result = status.result;
                timelinePage.resultStartDate = timelinePage.submittedStartDate;
                timelinePage.resultEndDate = timelinePage.submittedEndDate;
            } else if (status.state === "completed") {
                timelinePage.resultRequested = false;
                if (includedResult) {
                    timelinePage.retryCompletedResult("The completed timeline returned no result.");
                } else {
                    timelinePage.requestTimelineStatus(true);
                }
            }
        }

        function onTimelineStatusFailed(id, message, includedResult) {
            if (id !== timelinePage.jobId)
                return;
            timelinePage.statusRequestPending = false;
            if (message.indexOf("HTTP 404") === 0) {
                timelinePage.resultRequested = false;
                timelinePage.jobState = "failed";
                timelinePage.errorMessage = "The timeline is no longer available.";
                return;
            }
            if (includedResult) {
                timelinePage.retryCompletedResult("Could not load the completed timeline.");
                return;
            }
            timelinePage.errorMessage = "Timeline status is temporarily unavailable; retrying.";
        }

        function onGeodataStatusFetched(status) {
            timelinePage.geodataStatus = status;
        }

        function onImportCompleted(summary, params) {
            if (!summary || Number(summary.files || 0) === 0)
                return;
            timelinePage.invalidateCachedTimeline();
            if (timelinePage.active)
                Qt.callLater(timelinePage.beginTimeline);
        }

        function onGeodataInstallAccepted(generationId) {
            timelinePage.api.getGeodataStatus();
        }

        function onGeodataInstallFailed(generationId, message) {
            timelinePage.errorMessage = message;
            timelinePage.api.getGeodataStatus();
        }
    }

    Timer {
        interval: 250
        repeat: true
        running: timelinePage.active && timelinePage.api !== null && timelinePage.jobId !== "" && (timelinePage.jobState === "queued" || timelinePage.jobState === "running" || timelinePage.jobState === "cancelling")
        onTriggered: timelinePage.requestTimelineStatus(false)
    }

    Timer {
        id: resultRetryTimer
        interval: 1000
        repeat: false
        onTriggered: {
            if (timelinePage.active && timelinePage.jobState === "completed" && !timelinePage.result)
                timelinePage.requestTimelineStatus(true);
        }
    }

    Timer {
        interval: 500
        repeat: true
        running: timelinePage.active && timelinePage.api !== null && timelinePage.geodataStatus && timelinePage.geodataStatus.install && timelinePage.geodataStatus.install.state === "installing"
        onTriggered: timelinePage.api.getGeodataStatus()
    }

    header: ToolBar {
        id: timelineToolbar
        height: 50

        background: Rectangle {
            color: theme.toolbarBackground

            TapHandler {
                acceptedButtons: Qt.LeftButton
                gesturePolicy: TapHandler.WithinBounds
                onDoubleTapped: {
                    if (!timelinePage.hostWindow)
                        return;
                    timelinePage.hostWindow.visibility = timelinePage.hostWindow.visibility === Window.FullScreen ? Window.Windowed : Window.FullScreen;
                }
            }
        }

        DragHandler {
            target: null
            onActiveChanged: {
                if (active && timelinePage.hostWindow && timelinePage.hostWindow.startSystemMove)
                    timelinePage.hostWindow.startSystemMove();
            }
        }

        MouseArea {
            anchors.left: parent.left
            anchors.top: parent.top
            width: 18
            height: 18
            z: 10
            acceptedButtons: Qt.LeftButton
            cursorShape: Qt.SizeFDiagCursor
            onPressed: {
                if (timelinePage.hostWindow && timelinePage.hostWindow.startSystemResize)
                    timelinePage.hostWindow.startSystemResize(Qt.TopEdge | Qt.LeftEdge);
            }
        }

        MouseArea {
            anchors.right: parent.right
            anchors.top: parent.top
            width: 18
            height: 18
            z: 10
            acceptedButtons: Qt.LeftButton
            cursorShape: Qt.SizeBDiagCursor
            onPressed: {
                if (timelinePage.hostWindow && timelinePage.hostWindow.startSystemResize)
                    timelinePage.hostWindow.startSystemResize(Qt.TopEdge | Qt.RightEdge);
            }
        }

        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12

            ToolButton {
                text: "Back"
                Accessible.name: "Back to map"
                onClicked: timelinePage.leaveTimeline()
            }

            Label {
                Layout.fillWidth: true
                text: "Where was I?"
                color: theme.toolbarText
                font.bold: true
                font.pixelSize: theme.scale(3)
                horizontalAlignment: Text.AlignHCenter
            }

            ToolButton {
                id: timelineQuitButton
                icon.source: "qrc:/icons/quit.svg"
                icon.width: theme.toolbarButtonSize
                icon.height: theme.toolbarButtonSize
                icon.color: hovered ? theme.toolbarIconHover : theme.toolbarIcon
                Accessible.name: "Quit"
                CustomToolTip {
                    tooltipText: "Quit"
                    visible: timelineQuitButton.hovered
                    position: "bottom"
                }
                onClicked: timelinePage.quitRequested()
            }
        }
    }

    Item {
        anchors.fill: parent

        TimelineView {
            id: timelineViewContent
            anchors.fill: parent
            visible: timelinePage.result !== null
            result: timelinePage.result
            year: timelinePage.selectedYear
            active: timelinePage.active
            controlsVisible: timelinePage.currentView === 0
        }

        Rectangle {
            id: statusCard
            objectName: "timelineStatusCard"
            anchors.centerIn: parent
            width: Math.max(0, Math.min(520, parent.width - 32))
            height: statusContent.implicitHeight + 28
            radius: 16
            color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.96)
            visible: timelinePage.result === null && timelinePage.jobState !== "unavailable"
            z: 20

            RowLayout {
                id: statusContent
                anchors.fill: parent
                anchors.margins: 12
                spacing: 12

                BusyIndicator {
                    running: timelinePage.jobState === "submitting" || timelinePage.jobState === "queued" || timelinePage.jobState === "running" || timelinePage.jobState === "cancelling"
                    visible: running
                    Layout.preferredWidth: 30
                    Layout.preferredHeight: 30
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 2

                    Label {
                        Layout.fillWidth: true
                        text: {
                            if (timelinePage.jobState === "completed")
                                return timelinePage.result ? "Timeline ready" : "Timeline result unavailable";
                            if (timelinePage.jobState === "unavailable")
                                return "Administrative place data required";
                            if (timelinePage.jobState === "failed")
                                return "Timeline failed";
                            if (timelinePage.jobState === "cancelled")
                                return "Timeline cancelled";
                            if (timelinePage.jobState === "queued")
                                return "Timeline queued";
                            if (timelinePage.jobState === "cancelling")
                                return "Cancelling timeline";
                            return "Building your local timeline";
                        }
                        color: theme.toolbarText
                        font.bold: true
                        font.pixelSize: theme.scale(2)
                    }

                    Label {
                        Layout.fillWidth: true
                        text: timelinePage.errorMessage !== "" ? timelinePage.errorMessage : (timelinePage.processedObservations > 0 ? timelinePage.processedObservations + " recorded observations processed" : "Cached places are reused; missing places get foreground priority.")
                        color: theme.toolbarTextSecondary
                        font.pixelSize: theme.scale(2)
                        wrapMode: Text.WordWrap
                    }
                }

                Button {
                    visible: timelinePage.jobState === "queued" || timelinePage.jobState === "running" || timelinePage.jobState === "completed" || timelinePage.jobState === "failed" || timelinePage.jobState === "cancelled"
                    text: timelinePage.jobState === "queued" || timelinePage.jobState === "running" ? "Cancel" : (timelinePage.jobState === "completed" ? "Retry result" : "Retry")
                    onClicked: {
                        if (timelinePage.jobState === "queued" || timelinePage.jobState === "running") {
                            timelinePage.cancelActiveTimeline();
                        } else if (timelinePage.jobState === "completed") {
                            timelinePage.resultRetryCount = 0;
                            timelinePage.errorMessage = "";
                            timelinePage.requestTimelineStatus(true);
                        } else {
                            timelinePage.beginTimeline();
                        }
                    }
                }
            }
        }

        Rectangle {
            anchors.centerIn: parent
            width: Math.max(0, Math.min(560, parent.width - 32))
            height: unavailableContent.implicitHeight + 36
            radius: 16
            color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.97)
            visible: timelinePage.jobState === "unavailable"
            z: 20

            ColumnLayout {
                id: unavailableContent
                anchors.fill: parent
                anchors.margins: 18
                spacing: 12

                Label {
                    Layout.fillWidth: true
                    text: "Place boundaries are not installed"
                    color: theme.primaryText
                    font.bold: true
                    font.pixelSize: theme.scale(4)
                    horizontalAlignment: Text.AlignHCenter
                }
                Label {
                    Layout.fillWidth: true
                    text: timelinePage.geodataStatus && timelinePage.geodataStatus.available && timelinePage.geodataStatus.available.length > 0 ? "Install the verified offline boundary dataset to create this timeline." : "This build does not advertise a signed boundary dataset yet. No mutable upstream download will be used."
                    color: theme.secondaryText
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }
                ProgressBar {
                    Layout.fillWidth: true
                    visible: timelinePage.geodataStatus && timelinePage.geodataStatus.install && timelinePage.geodataStatus.install.state === "installing"
                    from: 0
                    to: timelinePage.geodataStatus && timelinePage.geodataStatus.install && timelinePage.geodataStatus.install.progress ? Math.max(1, timelinePage.geodataStatus.install.progress.total_bytes || 1) : 1
                    value: timelinePage.geodataStatus && timelinePage.geodataStatus.install && timelinePage.geodataStatus.install.progress ? timelinePage.geodataStatus.install.progress.bytes || 0 : 0
                }
                RowLayout {
                    Layout.alignment: Qt.AlignHCenter
                    Button {
                        visible: timelinePage.geodataStatus && timelinePage.geodataStatus.available && timelinePage.geodataStatus.available.length > 0 && (!timelinePage.geodataStatus.install || timelinePage.geodataStatus.install.state !== "installing")
                        text: "Install offline data"
                        onClicked: timelinePage.api.installGeodata(timelinePage.geodataStatus.available[0].id)
                    }
                    Button {
                        visible: timelinePage.geodataStatus && timelinePage.geodataStatus.install && timelinePage.geodataStatus.install.state === "installing"
                        text: "Cancel install"
                        onClicked: timelinePage.api.cancelGeodataInstall()
                    }
                    Button {
                        text: "Retry timeline"
                        onClicked: timelinePage.beginTimeline()
                    }
                }
            }
        }

        TimelineDetailsSheet {
            id: detailsOverlay
            anchors.fill: parent
            result: timelinePage.result
            period: timelinePage.timelinePeriod()
            open: timelinePage.currentView === 1
            topInset: yearPill.y + yearPill.height + 10
            z: 30
            onCloseRequested: {
                timelinePage.currentView = 0;
                Qt.callLater(detailsToggle.forceActiveFocus);
            }
            onPlaceRequested: place => timelinePage.showPlaceOnTimeline(place)
            onRefreshRequested: timelinePage.beginTimeline()
        }

        Rectangle {
            id: yearPill
            objectName: "timelineYearPill"
            anchors.top: parent.top
            anchors.left: parent.left
            anchors.topMargin: 16
            anchors.leftMargin: 16
            width: yearControls.implicitWidth + 8
            height: 42
            radius: 21
            color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.94)
            visible: timelinePage.currentView === 0
            z: 40

            RowLayout {
                id: yearControls
                anchors.fill: parent
                anchors.leftMargin: 4
                anchors.rightMargin: 4
                spacing: 0

                ToolButton {
                    id: previousYearButton
                    Layout.preferredWidth: 40
                    Layout.preferredHeight: 40
                    text: "‹"
                    Accessible.name: "Previous year"
                    enabled: timelinePage.selectedYear > 1
                    onClicked: timelinePage.yearRequested(timelinePage.selectedYear - 1)
                    contentItem: Text {
                        text: previousYearButton.text
                        color: previousYearButton.enabled ? theme.primaryText : theme.toolbarIconDisabled
                        font.bold: true
                        font.pixelSize: theme.scale(4)
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                    background: Rectangle {
                        radius: 20
                        color: previousYearButton.down ? theme.toolbarBackgroundHover : "transparent"
                    }
                }

                Label {
                    Layout.preferredWidth: 58
                    text: timelinePage.selectedYear
                    color: theme.primaryText
                    font.bold: true
                    font.pixelSize: theme.scale(2)
                    horizontalAlignment: Text.AlignHCenter
                }

                ToolButton {
                    id: nextYearButton
                    Layout.preferredWidth: 40
                    Layout.preferredHeight: 40
                    text: "›"
                    Accessible.name: "Next year"
                    enabled: timelinePage.selectedYear < timelinePage.currentUTCYear
                    onClicked: timelinePage.yearRequested(timelinePage.selectedYear + 1)
                    contentItem: Text {
                        text: nextYearButton.text
                        color: nextYearButton.enabled ? theme.primaryText : theme.toolbarIconDisabled
                        font.bold: true
                        font.pixelSize: theme.scale(4)
                        horizontalAlignment: Text.AlignHCenter
                        verticalAlignment: Text.AlignVCenter
                    }
                    background: Rectangle {
                        radius: 20
                        color: nextYearButton.down ? theme.toolbarBackgroundHover : "transparent"
                    }
                }
            }
        }

        Button {
            id: detailsToggle
            anchors.top: parent.top
            anchors.right: parent.right
            anchors.topMargin: 16
            anchors.rightMargin: 16
            width: 98
            height: 42
            visible: timelinePage.result !== null
            text: timelinePage.currentView === 0 ? "Details" : "Timeline"
            Accessible.name: timelinePage.currentView === 0 ? "Open timeline details" : "Return to timeline"
            z: 40
            onClicked: timelinePage.currentView = timelinePage.currentView === 0 ? 1 : 0
            contentItem: Text {
                text: detailsToggle.text
                color: theme.primaryText
                font.bold: true
                horizontalAlignment: Text.AlignHCenter
                verticalAlignment: Text.AlignVCenter
            }
            background: Rectangle {
                radius: 21
                color: detailsToggle.down ? theme.toolbarBackgroundHover : Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.94)
            }
        }

        MouseArea {
            anchors.left: parent.left
            anchors.bottom: parent.bottom
            width: 18
            height: 18
            z: 100
            acceptedButtons: Qt.LeftButton
            cursorShape: Qt.SizeBDiagCursor
            onPressed: {
                if (timelinePage.hostWindow && timelinePage.hostWindow.startSystemResize)
                    timelinePage.hostWindow.startSystemResize(Qt.BottomEdge | Qt.LeftEdge);
            }
        }

        MouseArea {
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            width: 18
            height: 18
            z: 100
            acceptedButtons: Qt.LeftButton
            cursorShape: Qt.SizeFDiagCursor
            onPressed: {
                if (timelinePage.hostWindow && timelinePage.hostWindow.startSystemResize)
                    timelinePage.hostWindow.startSystemResize(Qt.BottomEdge | Qt.RightEdge);
            }
        }
    }
}
