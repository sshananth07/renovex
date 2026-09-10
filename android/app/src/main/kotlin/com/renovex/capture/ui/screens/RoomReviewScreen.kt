package com.renovex.capture.ui.screens

import androidx.compose.foundation.Canvas
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.Button
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.testTag
import androidx.compose.ui.unit.dp
import com.renovex.capture.capture.RoomProposalStore
import com.renovex.capture.geometry.OpeningDraft
import com.renovex.capture.geometry.OpeningProfile
import com.renovex.capture.geometry.OpeningType
import com.renovex.capture.geometry.ObstacleDraft
import com.renovex.capture.geometry.ObstacleType
import com.renovex.capture.geometry.RoomDraft
import com.renovex.capture.geometry.ServicePointDraft
import com.renovex.capture.geometry.ServicePointType
import kotlin.math.max

/** Contractor-facing transactional editor for one frozen scanner proposal. */
@Composable
fun RoomReviewScreen(
    scanSessionId: String,
    proposalStore: RoomProposalStore,
    onContinueScanning: () -> Unit = {},
    onConfirmed: () -> Unit = {},
) {
    val controller = remember(scanSessionId, proposalStore) { RoomReviewController(proposalStore, scanSessionId) }
    var reviewState by remember { mutableStateOf(controller.state) }
    fun refresh() { reviewState = controller.state }

    val state = reviewState
    if (state == null) {
        Text("Room proposal unavailable", modifier = Modifier.padding(24.dp))
        return
    }
    val draft = state.workingCopy
    val measurements = state.measurements

    Column(
        modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState()).padding(20.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Text("Room review")
        Text("Review and correct the frozen scanner proposal before local confirmation.")
        RoomDraftPlan(draft, state.selectedCornerId, state.selectedWallId)

        Text("Floor area: ${format(measurements.floorAreaSquareMeters)} m²")
        Text("Perimeter: ${format(measurements.perimeterMeters)} m")
        Text(
            draft.ceilingHeight.meters?.let {
                "Ceiling height: ${format(it)} m · ${draft.ceilingHeight.status.name.lowercase().replace('_', ' ')}"
            } ?: "Ceiling height: Unconfirmed",
        )
        CeilingEditor(draft) { height -> controller.setCeilingHeight(height); refresh() }
        measurements.grossWallAreaSquareMeters?.let { Text("Gross wall area: ${format(it)} m²") }
        measurements.netWallAreaSquareMeters?.let { Text("Net wall area: ${format(it)} m²") }

        HorizontalDivider()
        Text("Boundary")
        draft.corners.forEachIndexed { index, corner ->
            OutlinedButton(
                onClick = { controller.selectCorner(corner.id); refresh() },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("Corner ${index + 1}${if (state.selectedCornerId == corner.id) " · Selected" else ""}")
            }
        }
        state.selectedCornerId?.let { selectedId ->
            val corner = draft.corners.firstOrNull { it.id == selectedId }
            if (corner != null) {
                val number = draft.corners.indexOf(corner) + 1
                var xText by remember(corner.id, corner.x) { mutableStateOf(corner.x.toString()) }
                var zText by remember(corner.id, corner.z) { mutableStateOf(corner.z.toString()) }
                Text("Edit corner $number")
                NumericField("X position (m)", xText, { xText = it }, "corner-x-input")
                NumericField("Z position (m)", zText, { zText = it }, "corner-z-input")
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = {
                        val x = xText.toDoubleOrNull()
                        val z = zText.toDoubleOrNull()
                        if (x != null && z != null) controller.moveSelectedCorner(x, z)
                        refresh()
                    }) { Text("Update corner") }
                    OutlinedButton(onClick = {
                        val x = xText.toDoubleOrNull()
                        val z = zText.toDoubleOrNull()
                        if (x != null && z != null) controller.addCornerAfterSelection(x, z)
                        refresh()
                    }) { Text("Add corner after") }
                    OutlinedButton(onClick = { controller.deleteSelectedCorner(); refresh() }) {
                        Text("Delete corner")
                    }
                }
            }
        }

        Text("Walls")
        draft.walls.forEachIndexed { index, wall ->
            val length = measurements.wallLengthsMeters[wall.id] ?: 0.0
            OutlinedButton(
                onClick = { controller.selectWall(wall.id); refresh() },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text("Wall ${index + 1} · ${format(length)} m")
            }
        }
        state.selectedWallId?.let { selectedId ->
            val selectedIndex = draft.walls.indexOfFirst { it.id == selectedId }
            if (selectedIndex >= 0) {
                Text("Edit wall ${selectedIndex + 1}")
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    OutlinedButton(onClick = { controller.splitSelectedWall(); refresh() }) { Text("Split wall") }
                    draft.walls.filter { it.id != selectedId }.forEachIndexed { index, wall ->
                        OutlinedButton(onClick = { controller.mergeSelectedWallWith(wall.id); refresh() }) {
                            Text("Merge with ${index + 1}")
                        }
                    }
                }
                AddOpeningEditor { type, profile, offset, width, height, spring, rise ->
                    controller.addOpeningToSelectedWall(
                        type, profile, offset, width, height,
                        archParameters = if (profile == OpeningProfile.ARCH && spring != null && rise != null) {
                            com.renovex.capture.geometry.ArchParameters(spring, rise)
                        } else null,
                    )
                    refresh()
                }
            }
        }

        HorizontalDivider()
        Text("Openings")
        draft.openings.forEachIndexed { index, opening ->
            OutlinedButton(
                onClick = { controller.selectOpening(opening.id); refresh() },
                modifier = Modifier.fillMaxWidth(),
            ) { Text("Opening ${index + 1} · ${opening.type.name.lowercase()} · ${format(opening.widthMeters)} m") }
        }
        state.selectedOpeningId?.let { id ->
            draft.openings.firstOrNull { it.id == id }?.let { opening ->
                OpeningEditor(
                    opening = opening,
                    onUpdate = { type, profile, offset, width, height, spring, rise ->
                        controller.updateOpening(id, type, profile, offset, width, height,
                            springHeightMeters = spring, archRiseMeters = rise)
                        refresh()
                    },
                    onRemove = { controller.removeOpening(id); refresh() },
                )
            }
        }

        HorizontalDivider()
        Text("Obstacles and service points")
        AddObstacleEditor { type, x, z, width, depth ->
            controller.addObstacle(type, x, z, width, depth); refresh()
        }
        draft.obstacles.forEachIndexed { index, obstacle ->
            OutlinedButton(onClick = { controller.selectObstacle(obstacle.id); refresh() }, modifier = Modifier.fillMaxWidth()) {
                Text("Obstacle ${index + 1} · ${obstacle.type.name.lowercase().replace('_', ' ')}")
            }
        }
        state.selectedObstacleId?.let { id ->
            draft.obstacles.firstOrNull { it.id == id }?.let { obstacle ->
                ObstacleEditor(obstacle, { type, x, z, width, depth ->
                    controller.updateObstacle(id, type, x, z, width, depth); refresh()
                }, { controller.removeObstacle(id); refresh() })
            }
        }
        AddServiceEditor { type, x, z -> controller.addServicePoint(type, x, z); refresh() }
        draft.servicePoints.forEachIndexed { index, service ->
            OutlinedButton(onClick = { controller.selectServicePoint(service.id); refresh() }, modifier = Modifier.fillMaxWidth()) {
                Text("Service ${index + 1} · ${service.type.name.lowercase()}")
            }
        }
        state.selectedServicePointId?.let { id ->
            draft.servicePoints.firstOrNull { it.id == id }?.let { service ->
                ServiceEditor(service, { type, x, z ->
                    controller.updateServicePoint(id, type, x, z); refresh()
                }, { controller.removeServicePoint(id); refresh() })
            }
        }

        HorizontalDivider()
        Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
            Button(
                onClick = { controller.applyChanges(); refresh() },
                enabled = state.hasUnappliedChanges,
            ) { Text("Apply changes") }
            OutlinedButton(
                onClick = { controller.cancelChanges(); refresh() },
                enabled = state.hasUnappliedChanges,
            ) { Text("Cancel changes") }
            OutlinedButton(onClick = { controller.resetToSnapshot(); refresh() }) { Text("Reset proposal") }
        }
        OutlinedButton(
            onClick = {
                if (controller.continueScanning()) {
                    refresh()
                    onContinueScanning()
                }
            },
            modifier = Modifier.fillMaxWidth(),
        ) { Text("Continue scanning") }
        Button(
            onClick = { if (controller.confirmLocally()) { refresh(); onConfirmed() } },
            enabled = !state.confirmed && !state.hasUnappliedChanges,
            modifier = Modifier.fillMaxWidth(),
        ) { Text(if (state.confirmed) "Room confirmed locally" else "Confirm room") }
    }
}

@Composable
private fun RoomDraftPlan(draft: RoomDraft, selectedCornerId: String?, selectedWallId: String?) {
    if (draft.corners.size < 2) return
    val minX = draft.corners.minOf { it.x }
    val maxX = draft.corners.maxOf { it.x }
    val minZ = draft.corners.minOf { it.z }
    val maxZ = draft.corners.maxOf { it.z }
    Box(Modifier.fillMaxWidth().height(260.dp).padding(8.dp)) {
        Canvas(Modifier.fillMaxSize()) {
            val scale = minOf(
                size.width / max(maxX - minX, 0.1),
                size.height / max(maxZ - minZ, 0.1),
            ) * 0.82f
            fun point(x: Double, z: Double): Offset = Offset(
                ((x - minX) * scale + size.width * 0.09f).toFloat(),
                (size.height - ((z - minZ) * scale + size.height * 0.09f)).toFloat(),
            )
            draft.walls.forEach { wall ->
                val start = draft.corners.firstOrNull { it.id == wall.startCornerId } ?: return@forEach
                val end = draft.corners.firstOrNull { it.id == wall.endCornerId } ?: return@forEach
                drawLine(
                    color = if (wall.id == selectedWallId) Color(0xFF6D4CC4) else Color(0xFF263238),
                    start = point(start.x, start.z),
                    end = point(end.x, end.z),
                    strokeWidth = if (wall.id == selectedWallId) 9f else 6f,
                )
            }
            draft.corners.forEach { corner ->
                drawCircle(
                    color = if (corner.id == selectedCornerId) Color(0xFFE65100) else Color.White,
                    radius = if (corner.id == selectedCornerId) 11f else 8f,
                    center = point(corner.x, corner.z),
                    style = Stroke(width = 5f),
                )
            }
        }
    }
}

@Composable
private fun CeilingEditor(draft: RoomDraft, onSet: (Double) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    var value by remember(draft.ceilingHeight.meters) { mutableStateOf(draft.ceilingHeight.meters?.toString().orEmpty()) }
    OutlinedButton(onClick = { expanded = !expanded }) { Text(if (expanded) "Close ceiling editor" else "Enter ceiling height") }
    if (expanded) {
        NumericField("Measured ceiling height (m)", value, { value = it }, "ceiling-height-input")
        Button(onClick = { value.toDoubleOrNull()?.let(onSet) }) { Text("Set ceiling height") }
    }
}

@Composable
private fun AddOpeningEditor(onAdd: (OpeningType, OpeningProfile, Double, Double, Double, Double?, Double?) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    OutlinedButton(onClick = { expanded = !expanded }) { Text(if (expanded) "Close opening editor" else "Add opening") }
    if (!expanded) return
    var offset by remember { mutableStateOf("0.1") }
    var width by remember { mutableStateOf("0.8") }
    var height by remember { mutableStateOf("2.0") }
    var type by remember { mutableStateOf(OpeningType.DOOR) }
    var profile by remember { mutableStateOf(OpeningProfile.RECTANGLE) }
    var spring by remember { mutableStateOf("1.7") }
    var rise by remember { mutableStateOf("0.3") }
    EnumButtons(OpeningType.entries, type) { type = it }
    EnumButtons(OpeningProfile.entries, profile) { profile = it }
    NumericField("Offset along wall (m)", offset, { offset = it })
    NumericField("Opening width (m)", width, { width = it })
    NumericField("Opening height (m)", height, { height = it })
    if (profile == OpeningProfile.ARCH) {
        NumericField("Spring height (m)", spring, { spring = it })
        NumericField("Arch rise (m)", rise, { rise = it })
    }
    Button(onClick = {
        val o = offset.toDoubleOrNull(); val w = width.toDoubleOrNull(); val h = height.toDoubleOrNull()
        if (o != null && w != null && h != null) onAdd(
            type, profile, o, w, h,
            spring.toDoubleOrNull().takeIf { profile == OpeningProfile.ARCH },
            rise.toDoubleOrNull().takeIf { profile == OpeningProfile.ARCH },
        )
    }) { Text("Add to working copy") }
}

@Composable
private fun OpeningEditor(
    opening: OpeningDraft,
    onUpdate: (OpeningType, OpeningProfile, Double, Double, Double, Double?, Double?) -> Unit,
    onRemove: () -> Unit,
) {
    var offset by remember(opening.id, opening.offsetAlongWallMeters) { mutableStateOf(opening.offsetAlongWallMeters.toString()) }
    var width by remember(opening.id, opening.widthMeters) { mutableStateOf(opening.widthMeters.toString()) }
    var height by remember(opening.id, opening.totalHeightMeters) { mutableStateOf(opening.totalHeightMeters.toString()) }
    var type by remember(opening.id, opening.type) { mutableStateOf(opening.type) }
    var profile by remember(opening.id, opening.profile) { mutableStateOf(opening.profile) }
    var spring by remember(opening.id) { mutableStateOf(opening.archParameters?.springHeightMeters?.toString() ?: "1.7") }
    var rise by remember(opening.id) { mutableStateOf(opening.archParameters?.archRiseMeters?.toString() ?: "0.3") }
    Text("Edit opening")
    EnumButtons(OpeningType.entries, type) { type = it }
    EnumButtons(OpeningProfile.entries, profile) { profile = it }
    NumericField("Offset along wall (m)", offset, { offset = it })
    NumericField("Opening width (m)", width, { width = it })
    NumericField("Opening height (m)", height, { height = it })
    if (profile == OpeningProfile.ARCH) {
        NumericField("Spring height (m)", spring, { spring = it })
        NumericField("Arch rise (m)", rise, { rise = it })
    }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Button(onClick = {
            val o = offset.toDoubleOrNull(); val w = width.toDoubleOrNull(); val h = height.toDoubleOrNull()
            if (o != null && w != null && h != null) onUpdate(
                type, profile, o, w, h,
                spring.toDoubleOrNull().takeIf { profile == OpeningProfile.ARCH },
                rise.toDoubleOrNull().takeIf { profile == OpeningProfile.ARCH },
            )
        }) { Text("Update opening") }
        OutlinedButton(onClick = onRemove) { Text("Remove opening") }
    }
}

@Composable
private fun AddObstacleEditor(onAdd: (ObstacleType, Double, Double, Double, Double) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    OutlinedButton(onClick = { expanded = !expanded }) { Text(if (expanded) "Close obstacle editor" else "Add obstacle") }
    if (!expanded) return
    var type by remember { mutableStateOf(ObstacleType.FIXED_OBSTACLE) }
    var x by remember { mutableStateOf("0.0") }; var z by remember { mutableStateOf("0.0") }
    var width by remember { mutableStateOf("0.2") }; var depth by remember { mutableStateOf("0.2") }
    EnumButtons(ObstacleType.entries, type) { type = it }
    NumericField("Obstacle X (m)", x, { x = it }); NumericField("Obstacle Z (m)", z, { z = it })
    NumericField("Obstacle width (m)", width, { width = it }); NumericField("Obstacle depth (m)", depth, { depth = it })
    Button(onClick = {
        val values = listOf(x, z, width, depth).map { it.toDoubleOrNull() }
        if (values.all { it != null }) onAdd(type, values[0]!!, values[1]!!, values[2]!!, values[3]!!)
    }) { Text("Add obstacle to working copy") }
}

@Composable
private fun ObstacleEditor(obstacle: ObstacleDraft, onUpdate: (ObstacleType, Double, Double, Double, Double) -> Unit, onRemove: () -> Unit) {
    var type by remember(obstacle.id, obstacle.type) { mutableStateOf(obstacle.type) }
    var x by remember(obstacle.id, obstacle.x) { mutableStateOf(obstacle.x.toString()) }
    var z by remember(obstacle.id, obstacle.z) { mutableStateOf(obstacle.z.toString()) }
    var width by remember(obstacle.id, obstacle.widthMeters) { mutableStateOf(obstacle.widthMeters.toString()) }
    var depth by remember(obstacle.id, obstacle.depthMeters) { mutableStateOf(obstacle.depthMeters.toString()) }
    Text("Edit obstacle")
    EnumButtons(ObstacleType.entries, type) { type = it }
    NumericField("Obstacle X (m)", x, { x = it }); NumericField("Obstacle Z (m)", z, { z = it })
    NumericField("Obstacle width (m)", width, { width = it }); NumericField("Obstacle depth (m)", depth, { depth = it })
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Button(onClick = {
            val values = listOf(x, z, width, depth).map { it.toDoubleOrNull() }
            if (values.all { it != null }) onUpdate(type, values[0]!!, values[1]!!, values[2]!!, values[3]!!)
        }) { Text("Update obstacle") }
        OutlinedButton(onClick = onRemove) { Text("Remove obstacle") }
    }
}

@Composable
private fun AddServiceEditor(onAdd: (ServicePointType, Double, Double) -> Unit) {
    var expanded by remember { mutableStateOf(false) }
    OutlinedButton(onClick = { expanded = !expanded }) { Text(if (expanded) "Close service editor" else "Add service point") }
    if (!expanded) return
    var type by remember { mutableStateOf(ServicePointType.ELECTRICAL) }
    var x by remember { mutableStateOf("0.0") }; var z by remember { mutableStateOf("0.0") }
    EnumButtons(ServicePointType.entries, type) { type = it }
    NumericField("Service X (m)", x, { x = it }); NumericField("Service Z (m)", z, { z = it })
    Button(onClick = {
        val px = x.toDoubleOrNull(); val pz = z.toDoubleOrNull()
        if (px != null && pz != null) onAdd(type, px, pz)
    }) { Text("Add service to working copy") }
}

@Composable
private fun ServiceEditor(service: ServicePointDraft, onUpdate: (ServicePointType, Double, Double) -> Unit, onRemove: () -> Unit) {
    var type by remember(service.id, service.type) { mutableStateOf(service.type) }
    var x by remember(service.id, service.x) { mutableStateOf(service.x.toString()) }
    var z by remember(service.id, service.z) { mutableStateOf(service.z.toString()) }
    Text("Edit service point")
    EnumButtons(ServicePointType.entries, type) { type = it }
    NumericField("Service X (m)", x, { x = it }); NumericField("Service Z (m)", z, { z = it })
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        Button(onClick = {
            val px = x.toDoubleOrNull(); val pz = z.toDoubleOrNull()
            if (px != null && pz != null) onUpdate(type, px, pz)
        }) { Text("Update service") }
        OutlinedButton(onClick = onRemove) { Text("Remove service") }
    }
}

@Composable
private fun <T : Enum<T>> EnumButtons(values: List<T>, selected: T, onSelect: (T) -> Unit) {
    Row(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
        values.forEach { value ->
            if (value == selected) Button(onClick = { onSelect(value) }) { Text(value.name.lowercase()) }
            else OutlinedButton(onClick = { onSelect(value) }) { Text(value.name.lowercase()) }
        }
    }
}

@Composable
private fun NumericField(label: String, value: String, onValueChange: (String) -> Unit, tag: String? = null) {
    OutlinedTextField(
        value = value,
        onValueChange = onValueChange,
        label = { Text(label) },
        modifier = Modifier.fillMaxWidth().let { if (tag == null) it else it.testTag(tag) },
        singleLine = true,
    )
}

private fun format(value: Double): String = "%.2f".format(value)
