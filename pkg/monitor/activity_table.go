package monitor

const (
	activityTableHeaderRows    = 1
	activityTableIndicatorRows = 1
)

type activityTableLayout struct {
	tableHeight     int
	dataRowsVisible int
}

func activityTableMetrics(panelHeight int) activityTableLayout {
	// Content height = panel height - title row (1) - border (2)
	contentHeight := panelHeight - 3

	// Reserve 1 line for scroll indicator for consistent layout.
	tableHeight := contentHeight - activityTableIndicatorRows
	if tableHeight < activityTableHeaderRows+1 {
		tableHeight = activityTableHeaderRows + 1
	}

	dataRowsVisible := tableHeight - activityTableHeaderRows
	if dataRowsVisible < 1 {
		dataRowsVisible = 1
	}

	return activityTableLayout{
		tableHeight:     tableHeight,
		dataRowsVisible: dataRowsVisible,
	}
}
