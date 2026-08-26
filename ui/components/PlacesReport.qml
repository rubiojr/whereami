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

    function hierarchy(place) {
        var parts = [];
        if (place.locality)
            parts.push(place.locality);
        if (place.local_admin && place.local_admin !== place.locality)
            parts.push(place.local_admin);
        if (place.county)
            parts.push(place.county);
        if (place.region)
            parts.push(place.region);
        if (place.country)
            parts.push(place.country);
        return parts.join(" · ");
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
        resultRequested = false;
        processedObservations = 0;
        api.submitPlaceReport(startDate, endDate, submissionId);
    }

    function leaveReport() {
        if (api && jobId !== "" && (jobState === "queued" || jobState === "running" || jobState === "cancelling"))
            api.cancelPlaceReport(jobId);
        submissionId = "";
        backRequested();
    }

    onActiveChanged: {
        if (active && !(jobState === "completed" && result && resultStartDate === startDate && resultEndDate === endDate))
            beginReport();
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
            report.api.getPlaceReport(id);
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
            if ((report.jobState === "completed" || report.jobState === "failed" || report.jobState === "cancelled")
                    && status.state !== report.jobState)
                return;
            report.jobState = status.state || "failed";
            report.errorMessage = status.error || "";
            report.processedObservations = status.processed_observations || 0;
            if (status.result) {
                report.result = status.result;
                report.resultStartDate = report.submittedStartDate;
                report.resultEndDate = report.submittedEndDate;
            } else if (status.state === "completed" && !report.resultRequested) {
                report.resultRequested = true;
                report.api.getPlaceReport(id, true);
            }
        }

        function onPlaceReportStatusFailed(id, message) {
            if (id !== report.jobId)
                return;
            report.jobState = "failed";
            report.errorMessage = message;
        }

        function onGeodataStatusFetched(status) {
            report.geodataStatus = status;
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
        onTriggered: report.api.getPlaceReport(report.jobId)
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
                text: "Places report"
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

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 24
        spacing: 14

        RowLayout {
            Layout.fillWidth: true
            spacing: 12

            ColumnLayout {
                Layout.fillWidth: true
                spacing: 2

                Label {
                    text: "Recorded places"
                    color: theme.primaryText
                    font.bold: true
                    font.pixelSize: theme.scale(6)
                }

                Label {
                    text: report.reportPeriod() + " · UTC"
                    color: theme.accent
                    font.pixelSize: theme.scale(2)
                }
            }

            RowLayout {
                spacing: 4

                ToolButton {
                    text: "‹"
                    Accessible.name: "Previous year"
                    enabled: report.selectedYear > 1
                    onClicked: report.yearRequested(report.selectedYear - 1)
                }

                Label {
                    Layout.preferredWidth: 58
                    text: report.selectedYear
                    color: theme.primaryText
                    font.bold: true
                    font.pixelSize: theme.scale(3)
                    horizontalAlignment: Text.AlignHCenter
                }

                ToolButton {
                    text: "›"
                    Accessible.name: "Next year"
                    enabled: report.selectedYear < report.currentUTCYear
                    onClicked: report.yearRequested(report.selectedYear + 1)
                }

                Button {
                    text: "This year"
                    visible: report.selectedYear !== report.currentUTCYear
                    onClicked: report.yearRequested(report.currentUTCYear)
                }
            }

            Button {
                text: "Run again"
                visible: report.jobState === "completed" || report.jobState === "failed" || report.jobState === "cancelled"
                onClicked: report.beginReport()
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: statusContent.implicitHeight + 24
            radius: 10
            color: theme.toolbarBackground
            border.color: theme.toolbarBorder

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
                                return "Report ready";
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
                            return "Resolving recorded observations locally";
                        }
                        color: theme.toolbarText
                        font.bold: true
                        font.pixelSize: theme.scale(2)
                    }

                    Label {
                        Layout.fillWidth: true
                        text: report.errorMessage !== "" ? report.errorMessage : (report.processedObservations > 0 ? report.processedObservations + " recorded observations processed" : "No online geocoding is used.")
                        color: theme.toolbarTextSecondary
                        wrapMode: Text.WordWrap
                    }
                }

                Button {
                    visible: report.jobState === "queued" || report.jobState === "running"
                    text: "Cancel"
                    onClicked: report.api.cancelPlaceReport(report.jobId)
                }
            }
        }

        GridLayout {
            Layout.fillWidth: true
            columns: report.width >= 760 ? 4 : 2
            columnSpacing: 10
            rowSpacing: 10
            visible: report.result !== null

            Repeater {
                model: report.result ? [
                    { label: "Recorded", value: report.result.summary.recorded_observations || 0 },
                    { label: "Resolved", value: report.result.summary.resolved_observations || 0 },
                    { label: "Outside boundaries", value: report.result.summary.unresolved_observations || 0 },
                    { label: "Invalid coordinates", value: report.result.summary.invalid_coordinates || 0 }
                ] : []

                delegate: Rectangle {
                    id: summaryCard
                    required property var modelData
                    Layout.fillWidth: true
                    Layout.preferredHeight: 72
                    radius: 8
                    color: theme.toolbarBackgroundHover
                    border.color: theme.toolbarSeparator

                    Column {
                        anchors.centerIn: parent
                        spacing: 2
                        Label {
                            anchors.horizontalCenter: parent.horizontalCenter
                            text: summaryCard.modelData.value
                            color: theme.primaryText
                            font.bold: true
                            font.pixelSize: theme.scale(4)
                        }
                        Label {
                            anchors.horizontalCenter: parent.horizontalCenter
                            text: summaryCard.modelData.label
                            color: theme.secondaryText
                            font.pixelSize: theme.scale(1)
                        }
                    }
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            radius: 10
            color: theme.toolbarBackground
            border.color: theme.toolbarBorder
            visible: report.result !== null

            ListView {
                id: placesList
                anchors.fill: parent
                anchors.margins: 1
                clip: true
                model: report.result ? report.result.places : []
                spacing: 1

                header: Rectangle {
                    width: placesList.width
                    height: 42
                    color: theme.toolbarBackgroundHover

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 16
                        anchors.rightMargin: 16
                        Label { Layout.fillWidth: true; text: "Administrative place"; color: theme.toolbarText; font.bold: true }
                        Label { Layout.preferredWidth: 110; text: "Observations"; color: theme.toolbarTextSecondary; horizontalAlignment: Text.AlignRight }
                        Label { Layout.preferredWidth: 70; text: "Days"; color: theme.toolbarTextSecondary; horizontalAlignment: Text.AlignRight }
                    }
                }

                delegate: Rectangle {
                    id: placeRow
                    required property var modelData
                    required property int index
                    width: placesList.width
                    height: 62
                    color: placeRow.index % 2 === 0 ? theme.toolbarBackground : theme.toolbarBackgroundHover

                    RowLayout {
                        anchors.fill: parent
                        anchors.leftMargin: 16
                        anchors.rightMargin: 16
                        spacing: 12

                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 2
                            Label {
                                Layout.fillWidth: true
                                text: placeRow.modelData.locality || placeRow.modelData.local_admin || placeRow.modelData.county || placeRow.modelData.region || placeRow.modelData.country || "Unnamed administrative area"
                                color: theme.primaryText
                                font.bold: true
                                font.pixelSize: theme.scale(2)
                                elide: Text.ElideRight
                            }
                            Label {
                                Layout.fillWidth: true
                                text: report.hierarchy(placeRow.modelData)
                                color: theme.secondaryText
                                font.pixelSize: theme.scale(1)
                                elide: Text.ElideRight
                            }
                        }
                        Label {
                            Layout.preferredWidth: 110
                            text: placeRow.modelData.recorded_observations
                            color: theme.toolbarText
                            font.bold: true
                            horizontalAlignment: Text.AlignRight
                        }
                        Label {
                            Layout.preferredWidth: 70
                            text: placeRow.modelData.recorded_days
                            color: theme.toolbarTextSecondary
                            horizontalAlignment: Text.AlignRight
                        }
                    }
                }

                Label {
                    anchors.centerIn: parent
                    visible: placesList.count === 0
                    text: "No administrative places were resolved for this period."
                    color: theme.secondaryText
                }
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.fillHeight: true
            radius: 10
            color: theme.toolbarBackground
            border.color: theme.toolbarBorder
            visible: report.jobState === "unavailable"

            ColumnLayout {
                anchors.centerIn: parent
                width: Math.min(520, parent.width - 48)
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

        Label {
            Layout.fillWidth: true
            visible: report.result !== null
            text: report.result ? "Recorded observations only · " + report.result.dataset.attribution + " · " + report.result.dataset.license : ""
            color: theme.secondaryText
            font.pixelSize: theme.scale(1)
            elide: Text.ElideRight
        }
    }

    footer: Rectangle {
        height: 30
        color: theme.background

        MouseArea {
            anchors.left: parent.left
            anchors.bottom: parent.bottom
            width: 18
            height: 18
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
            acceptedButtons: Qt.LeftButton
            cursorShape: Qt.SizeFDiagCursor
            onPressed: {
                if (report.hostWindow && report.hostWindow.startSystemResize)
                    report.hostWindow.startSystemResize(Qt.BottomEdge | Qt.RightEdge);
            }
        }
    }
}
