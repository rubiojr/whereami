pragma ComponentBehavior: Bound
import QtQuick 2.15
import QtQuick.Controls 2.15
import QtQuick.Layouts 1.15
import QtQuick.Window 2.15
import "../themes"

Page {
    id: report

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
    property alias timelineView: journeyTimeline
    property alias detailsPanel: detailsOverlay
    readonly property int currentUTCYear: new Date().getUTCFullYear()
    readonly property int selectedYear: {
        var year = Number(startDate.substring(0, 4));
        return isFinite(year) && year >= 1 ? year : currentUTCYear;
    }

    signal backRequested
    signal yearRequested(int year)

    padding: 0
    background: Rectangle {
        color: theme.background
    }

    ThemeLoader {
        id: theme
    }

    function reportPeriod() {
        return startDate === endDate ? startDate : startDate + " to " + endDate;
    }

    function beginReport() {
        if (!api || startDate === "" || endDate === "") {
            errorMessage = "Choose a valid date range.";
            jobState = "failed";
            return;
        }
        if (jobId !== "" && (jobState === "queued" || jobState === "running" || jobState === "cancelling"))
            api.cancelPlaceReport(jobId);
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
        api.submitPlaceReport(startDate, endDate, submissionId);
    }

    function requestReportStatus(includeResult) {
        if (!api || jobId === "" || statusRequestPending)
            return;
        statusRequestPending = true;
        resultRequested = includeResult === true;
        api.getPlaceReport(jobId, resultRequested);
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

    function leaveReport() {
        cancelActiveReport();
        currentView = 0;
        submissionId = "";
        backRequested();
    }

    function cancelActiveReport() {
        if (!api || jobId === "" || (jobState !== "queued" && jobState !== "running"))
            return;
        jobState = "cancelling";
        api.cancelPlaceReport(jobId);
    }

    function invalidateCachedReport() {
        cancelActiveReport();
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
            requestReportStatus(true);
        else if (active && !(jobState === "completed" && result && resultStartDate === startDate && resultEndDate === endDate))
            beginReport();
        else if (!active)
            cancelActiveReport();
    }

    Connections {
        target: report.api
        enabled: report.api !== null

        function onPlaceReportSubmitted(requestId, id) {
            if (!report.active || requestId !== report.submissionId) {
                report.api.cancelPlaceReport(id);
                return;
            }
            report.jobId = id;
            report.jobState = "queued";
            report.requestReportStatus(false);
        }

        function onPlaceReportSubmitFailed(requestId, message) {
            if (!report.active || requestId !== report.submissionId)
                return;
            report.jobState = "unavailable";
            report.errorMessage = message;
            report.api.getGeodataStatus();
        }

        function onPlaceReportStatusFetched(id, status) {
            if (id !== report.jobId)
                return;
            var includedResult = report.resultRequested;
            report.statusRequestPending = false;
            if (!status || typeof status !== "object") {
                report.resultRequested = false;
                report.errorMessage = "Report status response was invalid.";
                return;
            }
            if ((report.jobState === "completed" || report.jobState === "failed" || report.jobState === "cancelled")
                    && status.state !== report.jobState)
                return;
            report.jobState = status.state || "failed";
            report.errorMessage = status.error || "";
            report.processedObservations = status.processed_observations || 0;
            if (status.result) {
                report.resultRequested = false;
                report.resultRetryCount = 0;
                report.result = status.result;
                report.resultStartDate = report.submittedStartDate;
                report.resultEndDate = report.submittedEndDate;
            } else if (status.state === "completed") {
                report.resultRequested = false;
                if (includedResult) {
                    report.retryCompletedResult("The completed report returned no result.");
                } else {
                    report.requestReportStatus(true);
                }
            }
        }

        function onPlaceReportStatusFailed(id, message, includedResult) {
            if (id !== report.jobId)
                return;
            report.statusRequestPending = false;
            if (message.indexOf("HTTP 404") === 0) {
                report.resultRequested = false;
                report.jobState = "failed";
                report.errorMessage = "The place report is no longer available.";
                return;
            }
            if (includedResult) {
                report.retryCompletedResult("Could not load the completed report.");
                return;
            }
            report.errorMessage = "Report status is temporarily unavailable; retrying.";
        }

        function onGeodataStatusFetched(status) {
            report.geodataStatus = status;
        }

        function onImportCompleted(summary, params) {
            if (!summary || Number(summary.files || 0) === 0)
                return;
            report.invalidateCachedReport();
            if (report.active)
                Qt.callLater(report.beginReport);
        }

        function onGeodataInstallAccepted(generationId) {
            report.api.getGeodataStatus();
        }

        function onGeodataInstallFailed(generationId, message) {
            report.errorMessage = message;
            report.api.getGeodataStatus();
        }
    }

    Timer {
        interval: 250
        repeat: true
        running: report.active && report.api !== null && report.jobId !== "" && (report.jobState === "queued" || report.jobState === "running" || report.jobState === "cancelling")
        onTriggered: report.requestReportStatus(false)
    }

    Timer {
        id: resultRetryTimer
        interval: 1000
        repeat: false
        onTriggered: {
            if (report.active && report.jobState === "completed" && !report.result)
                report.requestReportStatus(true);
        }
    }

    Timer {
        interval: 500
        repeat: true
        running: report.active && report.api !== null && report.geodataStatus && report.geodataStatus.install && report.geodataStatus.install.state === "installing"
        onTriggered: report.api.getGeodataStatus()
    }

    header: ToolBar {
        id: reportToolbar
        height: 50

        background: Rectangle {
            color: theme.toolbarBackground

            TapHandler {
                acceptedButtons: Qt.LeftButton
                gesturePolicy: TapHandler.WithinBounds
                onDoubleTapped: {
                    if (!report.hostWindow)
                        return;
                    report.hostWindow.visibility = report.hostWindow.visibility === Window.FullScreen ? Window.Windowed : Window.FullScreen;
                }
            }
        }

        DragHandler {
            target: null
            onActiveChanged: {
                if (active && report.hostWindow && report.hostWindow.startSystemMove)
                    report.hostWindow.startSystemMove();
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
                if (report.hostWindow && report.hostWindow.startSystemResize)
                    report.hostWindow.startSystemResize(Qt.TopEdge | Qt.LeftEdge);
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
                if (report.hostWindow && report.hostWindow.startSystemResize)
                    report.hostWindow.startSystemResize(Qt.TopEdge | Qt.RightEdge);
            }
        }

        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 12
            anchors.rightMargin: 12

            ToolButton {
                text: "Back"
                Accessible.name: "Back to map"
                onClicked: report.leaveReport()
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
                text: "Quit"
                Accessible.name: "Quit"
                onClicked: Qt.quit()
            }
        }
    }

    Item {
        anchors.fill: parent

        TravelTimeline {
            id: journeyTimeline
            anchors.fill: parent
            visible: report.result !== null
            result: report.result
            year: report.selectedYear
            active: report.active
            controlsVisible: report.currentView === 0
        }

        Rectangle {
            id: statusCard
            objectName: "placesStatusCard"
            anchors.centerIn: parent
            width: Math.max(0, Math.min(520, parent.width - 32))
            height: statusContent.implicitHeight + 28
            radius: 16
            color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.96)
            visible: report.result === null && report.jobState !== "unavailable"
            z: 20

            RowLayout {
                id: statusContent
                anchors.fill: parent
                anchors.margins: 12
                spacing: 12

                BusyIndicator {
                    running: report.jobState === "submitting" || report.jobState === "queued" || report.jobState === "running" || report.jobState === "cancelling"
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
                            if (report.jobState === "completed")
                                return report.result ? "Report ready" : "Report result unavailable";
                            if (report.jobState === "unavailable")
                                return "Administrative place data required";
                            if (report.jobState === "failed")
                                return "Report failed";
                            if (report.jobState === "cancelled")
                                return "Report cancelled";
                            if (report.jobState === "queued")
                                return "Report queued";
                            if (report.jobState === "cancelling")
                                return "Cancelling report";
                            return "Building your local journey";
                        }
                        color: theme.toolbarText
                        font.bold: true
                        font.pixelSize: theme.scale(2)
                    }

                    Label {
                        Layout.fillWidth: true
                        text: report.errorMessage !== "" ? report.errorMessage : (report.processedObservations > 0 ? report.processedObservations + " recorded observations processed" : "Cached places are reused; missing places get foreground priority.")
                        color: theme.toolbarTextSecondary
                        wrapMode: Text.WordWrap
                    }
                }

                Button {
                    visible: report.jobState === "queued" || report.jobState === "running" || report.jobState === "completed" || report.jobState === "failed" || report.jobState === "cancelled"
                    text: report.jobState === "queued" || report.jobState === "running" ? "Cancel" : (report.jobState === "completed" ? "Retry result" : "Retry")
                    onClicked: {
                        if (report.jobState === "queued" || report.jobState === "running") {
                            report.cancelActiveReport();
                        } else if (report.jobState === "completed") {
                            report.resultRetryCount = 0;
                            report.errorMessage = "";
                            report.requestReportStatus(true);
                        } else {
                            report.beginReport();
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
            visible: report.jobState === "unavailable"
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
                    text: report.geodataStatus && report.geodataStatus.available && report.geodataStatus.available.length > 0 ? "Install the verified offline boundary dataset to create this report." : "This build does not advertise a signed boundary dataset yet. No mutable upstream download will be used."
                    color: theme.secondaryText
                    wrapMode: Text.WordWrap
                    horizontalAlignment: Text.AlignHCenter
                }
                ProgressBar {
                    Layout.fillWidth: true
                    visible: report.geodataStatus && report.geodataStatus.install && report.geodataStatus.install.state === "installing"
                    from: 0
                    to: report.geodataStatus && report.geodataStatus.install && report.geodataStatus.install.progress ? Math.max(1, report.geodataStatus.install.progress.total_bytes || 1) : 1
                    value: report.geodataStatus && report.geodataStatus.install && report.geodataStatus.install.progress ? report.geodataStatus.install.progress.bytes || 0 : 0
                }
                RowLayout {
                    Layout.alignment: Qt.AlignHCenter
                    Button {
                        visible: report.geodataStatus && report.geodataStatus.available && report.geodataStatus.available.length > 0 && (!report.geodataStatus.install || report.geodataStatus.install.state !== "installing")
                        text: "Install offline data"
                        onClicked: report.api.installGeodata(report.geodataStatus.available[0].id)
                    }
                    Button {
                        visible: report.geodataStatus && report.geodataStatus.install && report.geodataStatus.install.state === "installing"
                        text: "Cancel install"
                        onClicked: report.api.cancelGeodataInstall()
                    }
                    Button {
                        text: "Retry report"
                        onClicked: report.beginReport()
                    }
                }
            }
        }

        PlacesDetailsSheet {
            id: detailsOverlay
            anchors.fill: parent
            result: report.result
            period: report.reportPeriod()
            open: report.currentView === 1
            topInset: yearPill.y + yearPill.height + 10
            z: 30
            onCloseRequested: {
                report.currentView = 0;
                Qt.callLater(detailsToggle.forceActiveFocus);
            }
            onRefreshRequested: report.beginReport()
        }

        Rectangle {
            id: yearPill
            objectName: "placesYearPill"
            anchors.top: parent.top
            anchors.left: parent.left
            anchors.topMargin: 16
            anchors.leftMargin: 16
            width: yearControls.implicitWidth + 8
            height: 42
            radius: 21
            color: Qt.rgba(theme.toolbarBackground.r, theme.toolbarBackground.g, theme.toolbarBackground.b, 0.94)
            visible: report.currentView === 0
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
                    enabled: report.selectedYear > 1
                    onClicked: report.yearRequested(report.selectedYear - 1)
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
                    text: report.selectedYear
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
                    enabled: report.selectedYear < report.currentUTCYear
                    onClicked: report.yearRequested(report.selectedYear + 1)
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
            visible: report.result !== null
            text: report.currentView === 0 ? "Details" : "Journey"
            Accessible.name: report.currentView === 0 ? "Open report details" : "Return to journey"
            z: 40
            onClicked: report.currentView = report.currentView === 0 ? 1 : 0
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
                if (report.hostWindow && report.hostWindow.startSystemResize)
                    report.hostWindow.startSystemResize(Qt.BottomEdge | Qt.LeftEdge);
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
                if (report.hostWindow && report.hostWindow.startSystemResize)
                    report.hostWindow.startSystemResize(Qt.BottomEdge | Qt.RightEdge);
            }
        }
    }
}
